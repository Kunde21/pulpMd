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
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Kunde21/markdownfmt/markdown"
	"github.com/bmatcuk/doublestar"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	bf "gopkg.in/russross/blackfriday.v2"
)

var rootCmd = &cobra.Command{
	Use:   "pulpMd",
	Short: "Inject code snippets into markdown files",
	Long: `Pulp Markdown is a code injector for your markdown files.
Create and test your example code, then load it into your markdown pages.
Useful when generating documentation and creating tutorials.`,
	Run: func(cmd *cobra.Command, args []string) {
		cInj.injectCode()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

var codeTags = map[string]string{
	".sh":  "shell",
	".cpp": "c++",
}

var (
	cfgFile string
	norecur bool
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
	// TODO: Add capability to parse and insert markdown snippets.
	//flags.BoolVarP(&cInj.quoteMd, "block", "b", false, "Insert markdown as code block.")
	flags.BoolVarP(&cInj.leaveQuotes, "quotes", "q", false,
		"Leave block quote when no code was inserted below it.")

	//cobra.MarkFlagFilename(persistent, "inject")
	cobra.MarkFlagFilename(flags, "output")
	cobra.MarkFlagFilename(flags, "target")
	cobra.MarkFlagRequired(flags, "target")
}

type codeInj struct {
	mdExt       bf.Extensions
	target      string
	inject      string
	injectDir   string
	output      string
	extensions  []string
	leaveTags   bool
	leaveQuotes bool
	quoteMd     bool
	snip        *regexp.Regexp
	unlinkSet   []*bf.Node
}

func NewCodeInject() *codeInj {
	return &codeInj{
		snip:      regexp.MustCompile(`{{\s*snippet ([^ \t]+)\s*(\[(.*)\])?\s*}}`),
		unlinkSet: make([]*bf.Node, 0),
	}
}

// initConfig reads in config file and ENV variables if set.
func (ci *codeInj) initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
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
}

func (ci *codeInj) injectCode() {
	nodes := ci.Parse()
	nodes.Walk(ci.Inject)
	cInj.Unlink()
	cInj.Render(nodes)
}

func (ci *codeInj) Parse() *bf.Node {
	f, err := ioutil.ReadFile(ci.target)
	if err != nil {
		log.Fatal(err)
	}
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
	ci.mdExt = bf.FencedCode | bf.Tables | bf.HeadingIDs
	return bf.New(bf.WithExtensions(ci.mdExt)).Parse(f)
}

func (ci *codeInj) Inject(n *bf.Node, entering bool) bf.WalkStatus {
	// snippet tags will only be in text nodes
	if !entering || n.Type != bf.Text || !ci.snip.Match(n.Literal) ||
		n.Parent.Type != bf.Paragraph || n.Parent.Parent.Type != bf.Document {
		return bf.GoToNext
	}

	strs := ci.snip.FindSubmatch(n.Literal)
	mth, exts := strs[1], strings.Split(string(strs[3]), ",")
	if len(mth) < 2 {
		return bf.GoToNext
	}
	pattern := fmt.Sprintf("%s/%s.*", ci.injectDir, mth)
	matches, err := doublestar.Glob(pattern)
	if err != nil {
		log.Fatal(err)
	}
	var count int
	for _, v := range matches {
		node, err := ci.createNode(v, exts)
		if err != nil {
			fmt.Println(err)
		}
		switch {
		case node == nil:
		case node.Type == bf.CodeBlock:
			n.Parent.InsertBefore(node)
			count++
		case node.Type == bf.Document:
			cn := node.FirstChild.Next
			for ; cn != nil; cn = cn.Next {
				n.Parent.InsertBefore(cn.Prev)
			}
			n.Parent.InsertBefore(node.LastChild)
			count++
		default:
		}
	}
	ci.UnlinkNode(n, count)
	return bf.GoToNext
}

func (ci *codeInj) UnlinkNode(node *bf.Node, count int) {
	// Remove leading blockquote if no code was added.
	if !ci.leaveQuotes && count == 0 && node.Parent.Prev.Type == bf.BlockQuote {
		ci.unlinkSet = append(ci.unlinkSet, node.Parent.Prev)
	}
	// Remove snippet insert tag.
	if !ci.leaveTags {
		ci.unlinkSet = append(ci.unlinkSet, node.Parent)
	}
}

func (ci codeInj) Unlink() {
	for _, n := range ci.unlinkSet {
		n.Unlink()
	}
}

func (ci codeInj) Render(nodes *bf.Node) {
	if ci.output == "" {
		ci.output = ci.target
	}
	out, err := os.Create(ci.output)
	if err != nil {
		log.Fatal(err)
	}
	render := markdown.NewRenderer(nil)
	nodes.Walk(func(n *bf.Node, entering bool) bf.WalkStatus {
		return render.RenderNode(out, n, entering)
	})
	out.Close()
}

func (ci *codeInj) createNode(file string, exts []string) (node *bf.Node, err error) {
	tag, ok := codeTags[filepath.Ext(file)]
	if !ok {
		tag = strings.TrimPrefix(filepath.Ext(file), ".")
	}
	// Not in extension filter list
	if len(exts) > 0 && !inSlice(tag, exts) {
		return nil, nil
	}
	inject, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, errors.Wrapf(err, "read code file %s", file)
	}
	if tag == "md" {
		return bf.New(bf.WithExtensions(ci.mdExt)).Parse(inject), nil
	}
	node = bf.NewNode(bf.CodeBlock)
	node.CodeBlockData = bf.CodeBlockData{
		IsFenced:    true,
		FenceChar:   '`',
		FenceLength: 3,
		Info:        []byte(tag),
	}
	node.Literal = inject
	return node, nil
}

func inSlice(needle string, haystack []string) bool {
	for _, v := range haystack {
		if needle == strings.TrimSpace(v) {
			return true
		}
	}
	return false
}
