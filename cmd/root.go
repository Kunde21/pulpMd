// Copyright © 2018 Chad Kunde <Kunde21@gmail.com>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Kunde21/markdownfmt/v2/markdown"
	"github.com/bmatcuk/doublestar"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

var rootCmd = &cobra.Command{
	Use:   "pulpMd",
	Short: "Inject code snippets into markdown files",
	Long: `Pulp Markdown is a code injector for your markdown files.
Create and test your example code, then load it into your markdown pages.

Useful when generating documentation and creating tutorials.`,
	Run: func(cmd *cobra.Command, args []string) {
		err := cInj.injectCode()
		if err != nil {
			log.Fatal(err)
		}
	},
}

func Execute() (code int) {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		return 1
	}
	return 0
}

var codeTags = map[string]string{
	"sh":  "shell",
	"cpp": "c++",
}

var (
	cfgFile string
	norecur bool
	stdin   bool
	cInj    *codeInj
)

func init() {
	cInj = NewCodeInject()
	cobra.OnInitialize(cInj.initConfig)
	flags := rootCmd.PersistentFlags()

	flags.StringVar(&cfgFile, "config", "", "config file (default is $HOME/.pulpMd.yaml)")

	flags.StringVarP(&cInj.target, "target", "t", "", "Markdown target file")
	// TODO: Add fenced snippet parsing.
	//persistent.StringVarP(&cInj.inject, "inject", "i", "", "Code file to source snippets")
	flags.StringVarP(&cInj.injectDir, "injectDir", "d", ".", "Code directory to source snippets")
	flags.BoolVarP(&norecur, "norecur", "r", false, "Don't search injectDir recursively")
	flags.StringVarP(&cInj.output, "output", "o", "", "Output markdown file")
	flags.StringArrayVarP(&cInj.extensions, "fileExt", "e", nil, "File extensions to inject")
	flags.BoolVarP(&cInj.leaveTags, "notags", "n", false, "Leave snippet tags in markdown file.")
	// TODO: Add capability to parse and insert markdown snippets as code blocks.
	//flags.BoolVarP(&cInj.quoteMd, "block", "b", false, "Insert markdown as code block.")
	flags.BoolVarP(&cInj.leaveQuotes, "quotes", "q", false,
		"Leave block quote when no code was inserted below it.")
	flags.BoolVarP(&stdin, "stdin", "s", false, "Pass markdown file via stdin")

	//cobra.MarkFlagFilename(persistent, "inject")
	cobra.MarkFlagFilename(flags, "output")
	cobra.MarkFlagFilename(flags, "target")
}

type codeInj struct {
	target      string
	file        []byte
	inject      string
	injectDir   string
	output      string
	extensions  []string
	leaveTags   bool
	leaveQuotes bool
	quoteMd     bool
	snip        *regexp.Regexp
	unlinkSet   []ast.Node

	originSource []byte
	mapSources   map[ast.Node][]byte
}

func NewCodeInject() *codeInj {
	return &codeInj{
		snip:       regexp.MustCompile(`{{\s*snippet ([^ \t]+)\s*(\[(.*)\])?\s*}}`),
		unlinkSet:  make([]ast.Node, 0),
		mapSources: make(map[ast.Node][]byte),
	}
}

// initConfig reads in config file and ENV variables if set.
func (ci *codeInj) initConfig() {
	err := ci.initConfigErrors()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func (ci *codeInj) initConfigErrors() error {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			return err
		}

		// Search config in home directory with name ".pulpMd" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".pulpMd")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
	ci.injectDir = strings.TrimRight(ci.injectDir, "/")
	if !norecur {
		ci.injectDir = ci.injectDir + "/**"
	}

	if cInj.target == "" && stdin == false {
		rootCmd.Usage()
		return errors.New("'target' or 'stdin' is required")
	}

	if cInj.target != "" && stdin == true {
		rootCmd.Usage()
		return errors.New("'target' and 'stdin' cannot be used simultaneously")
	}

	if stdin {
		b, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		ci.file = b
	}
	return nil
}

func (ci *codeInj) injectCode() error {
	nodes, err := ci.Parse()
	if err != nil {
		return err
	}
	err = ast.Walk(nodes, ci.Inject)
	if err != nil {
		return err
	}

	cInj.Unlink()
	err = cInj.Render(nodes)
	if err != nil {
		return err
	}
	return nil
}

func (ci *codeInj) Parse() (ast.Node, error) {
	var f []byte
	if ci.target != "" {
		var err error
		f, err = ioutil.ReadFile(ci.target)
		if err != nil {
			return nil, err
		}
	}
	if len(ci.file) > 0 {
		f = ci.file
	}
	ci.originSource = f
	if len(ci.extensions) != 0 {
		ci.extensions = strings.Split(ci.extensions[0], ",")
		for k, v := range codeTags {
			if !inSlice(k, ci.extensions) && !inSlice(v, ci.extensions) {
				delete(codeTags, k)
			}
			k = strings.TrimPrefix(k, ".")
			if !inSlice(k, ci.extensions) && !inSlice(v, ci.extensions) {
				delete(codeTags, k)
			}
		}
		var count int
		for _, v := range ci.extensions {
			tags := strings.Split(v, ":")
			if len(tags) == 2 {
				codeTags[tags[0]] = tags[1]
				count++
			}
		}
		if count == len(ci.extensions) {
			ci.extensions = nil
		}
	}
	return markdown.NewParser().Parse(text.NewReader(f)), nil
}

// snippet is split to more siblings
func (ci *codeInj) IsMatchWithSiblings(n ast.Node) (bool, []byte, []ast.Node) {
	var nodes []ast.Node
	var joined []byte
	i := n
	for true {
		if i == nil {
			return false, nil, nil
		}
		tnode, is := i.(*ast.Text)
		if !is {
			return false, nil, nil
		}
		literal := tnode.Segment.Value(ci.originSource)
		joined = append(joined, literal...)
		nodes = append(nodes, tnode)
		if ci.snip.Match(joined) {
			return true, joined, nodes
		}

		i = i.NextSibling()
	}

	return false, nil, nil
}

func (ci *codeInj) Inject(n ast.Node, entering bool) (ast.WalkStatus, error) {
	// snippet tags will only be in text nodes
	if !entering {
		return ast.WalkContinue, nil
	}

	isMatch, literal, nodes := ci.IsMatchWithSiblings(n)
	if !isMatch ||
		n.Parent().Kind() != ast.KindParagraph || n.Parent().Parent().Kind() != ast.KindDocument {
		return ast.WalkContinue, nil
	}

	strs := ci.snip.FindSubmatch(literal)
	mth, exts := strs[1], strings.Split(string(strs[3]), ",")
	if len(mth) < 2 {
		return ast.WalkContinue, nil
	}
	hasExts := string(strs[2]) != ""
	pattern := fmt.Sprintf("%s/%s.*", ci.injectDir, mth)
	matches, err := doublestar.Glob(pattern)
	if err != nil {
		return ast.WalkStop, err
	}
	var count int
	var ignoreExt bool

	// if there is no extension, print files alhabetically
	// if there is extensions, print in written order
	// note: {{snipped FileName []}} is different semantic than  {{snipped FileName}}
	if !hasExts {
		sort.Strings(matches)
		ignoreExt = true
	}
	for _, ext := range exts {
		ext = strings.TrimSpace(ext)
		for _, v := range matches {
			node, err := ci.createNode(v, ext, ignoreExt)
			if err != nil {
				return ast.WalkStop, err
			}
			switch {
			case node == nil:
			case node.Kind() == ast.KindCodeBlock:
				n.Parent().Parent().InsertBefore(n.Parent().Parent(), n.Parent(), node)
				count++
			case node.Kind() == ast.KindDocument:
				cn := node.FirstChild().NextSibling()
				for ; cn != nil; cn = cn.NextSibling() {
					n.Parent().Parent().InsertBefore(n.Parent().Parent(), n.Parent(), cn.PreviousSibling())
				}
				n.Parent().Parent().InsertBefore(n.Parent().Parent(), n.Parent(), node.LastChild())
				count++
			default:
			}
		}
	}
	ci.UnlinkNode(nodes, count)
	return ast.WalkContinue, nil
}

func (ci *codeInj) UnlinkNode(nodes []ast.Node, count int) {
	// Remove leading blockquote if no code was added.
	node := nodes[0]
	if !ci.leaveQuotes && count == 0 && node.Parent().PreviousSibling().Kind() == ast.KindBlockquote {
		ci.unlinkSet = append(ci.unlinkSet, node.Parent().PreviousSibling())
	}
	// Remove snippet insert tag.
	if !ci.leaveTags {
		ci.unlinkSet = append(ci.unlinkSet, node.Parent())
	}
}

func (ci codeInj) Unlink() {
	for _, n := range ci.unlinkSet {
		n.Parent().RemoveChild(n.Parent(), n)
	}
}

func (ci codeInj) Render(nodes ast.Node) error {
	var out *os.File
	if ci.output == "" {
		if ci.target != "" {
			ci.output = ci.target
		}
		if len(ci.file) > 0 {
			out = os.Stdout
		}
	}
	if ci.output != "" {
		var err error
		out, err = os.Create(ci.output)
		if err != nil {
			return err
		}
	}
	defer out.Close()
	buffer := bytes.NewBuffer(nil)
	render := markdown.NewRenderer()
	err := ast.Walk(nodes, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		source, has := ci.mapSources[n]
		if has {
			return render.RenderSingle(buffer, source, n, entering), nil
		} else {
			return render.RenderSingle(buffer, ci.originSource, n, entering), nil
		}
	})
	if err != nil {
		return err
	}
	_, err = out.Write(bytes.TrimLeft(buffer.Bytes(), "\n"))
	if err != nil {
		return err
	}
	return nil
}

func (ci *codeInj) createNode(file string, ext string, ignoreExt bool) (ast.Node, error) {
	tag := strings.TrimPrefix(filepath.Ext(file), ".")
	// Not in extension filter list
	if tag != ext && !ignoreExt {
		return nil, nil
	}

	if _, ok := codeTags[tag]; ok {
		tag = codeTags[tag]
	}

	inject, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, errors.Wrapf(err, "read code file %s", file)
	}
	if tag != "md" {
		// fake source here
		source := "```" + tag + "\n"
		source += string(inject)
		source += "\n```"
		inject = []byte(source)
	}

	nodes := markdown.NewParser().Parse(text.NewReader(inject))
	err = ast.Walk(nodes, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		ci.mapSources[n] = inject
		return ast.WalkContinue, nil
	})
	if err != nil {
		return nil, err
	}

	return nodes, nil
}

func inSlice(needle string, haystack []string) bool {
	for _, v := range haystack {
		if needle == strings.TrimSpace(v) {
			return true
		}
	}
	return false
}
