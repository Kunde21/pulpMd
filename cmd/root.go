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
Useful when creating documentation and tutorials.`,
	Run: injectCode,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

var (
	cfgFile     string
	target      string
	inject      string
	injectDir   string
	output      string
	extensions  []string
	leaveTags   bool
	leaveQuotes bool
)

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.pulpMd.yaml)")

	rootCmd.PersistentFlags().StringVarP(&target, "target", "t", "", "Markdown target file")
	rootCmd.PersistentFlags().StringVarP(&inject, "inject", "i", ".", "Code file to source snippets")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "", "Output markdown file")
	rootCmd.PersistentFlags().StringVarP(&injectDir, "injectDir", "d", ".", "Code directory to source snippets")
	rootCmd.PersistentFlags().StringArrayVarP(&extensions, "fileExt", "e", nil, "File extensions to inject")
	rootCmd.PersistentFlags().BoolVarP(&leaveTags, "notags", "n", false, "Don't delete snippet insert tags in markdown file.")
	rootCmd.PersistentFlags().BoolVarP(&leaveQuotes, "noquotes", "q", false, "Don't delete block quote when no code was inserted below it.")

	cobra.MarkFlagFilename(rootCmd.PersistentFlags(), "target")
	cobra.MarkFlagFilename(rootCmd.PersistentFlags(), "inject")
	cobra.MarkFlagRequired(rootCmd.PersistentFlags(), "target")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
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
}

var codeTags = map[string]string{
	".go":   "go",
	".js":   "js",
	".json": "json",
	".sh":   "shell",
}

func injectCode(cmd *cobra.Command, args []string) {
	cmd.Flags().Parse(args)
	regex := regexp.MustCompile(`{{\s*snippet ([^ \t]+)\s*(\[(.*)\])?\s*}}`)
	f, err := ioutil.ReadFile(inject)
	if err != nil {
		log.Fatal(err)
	}
	if len(extensions) != 0 {
		for k, v := range codeTags {
			if !inSlice(k, extensions) && !inSlice(v, extensions) {
				delete(codeTags, k)
			}
		}
	}
	md := bf.New(bf.WithExtensions(bf.FencedCode | bf.Tables | bf.HeadingIDs))
	unlinkSet := make([]*bf.Node, 0)
	nodes := md.Parse(f)
	nodes.Walk(func(n *bf.Node, entering bool) bf.WalkStatus {
		// snippet tags will only be in text nodes
		if !entering || n.Type != bf.Text || !regex.Match(n.Literal) ||
			n.Parent.Type != bf.Paragraph || n.Parent.Parent.Type != bf.Document {
			return bf.GoToNext
		}

		strs := regex.FindSubmatch(n.Literal)
		mth, exts := strs[1], strings.Split(string(strs[3]), ",")
		if len(mth) < 2 {
			return bf.GoToNext
		}
		matches, err := filepath.Glob(fmt.Sprintf("%s.*", mth))
		if err != nil {
			log.Fatal(err)
		}
		var count int
		for _, v := range matches {
			node, err := codeNode(v, exts)
			if err != nil {
				log.Println(err)
			}
			if node == nil {
				continue
			}
			n.Parent.InsertBefore(node)
			count++
		}
		if !leaveQuotes && count == 0 && n.Parent.Prev.Type == bf.BlockQuote {
			// Remove leading blockquote if no code was added.
			unlinkSet = append(unlinkSet, n.Parent.Prev)
		}
		if !leaveTags {
			// Remove snippet insert tag.
			unlinkSet = append(unlinkSet, n.Parent)
		}
		return bf.GoToNext
	})
	for _, n := range unlinkSet {
		n.Unlink()
	}
}

type codeInj struct {
	snip      *regexp.Regexp
	unlinkSet []*bf.Node
}

func (ci codeInj) Render(nodes *bf.Node) {
	if output == "" {
		output = target
	}
	out, err := os.Create(output)
	if err != nil {
		log.Fatal(err)
	}
	render := markdown.NewRenderer(nil)
	nodes.Walk(func(n *bf.Node, entering bool) bf.WalkStatus {
		return render.RenderNode(out, n, entering)
	})
	out.Close()
}

func codeNode(file string, exts []string) (node *bf.Node, err error) {
	tag, ok := codeTags[filepath.Ext(file)]
	// Not supported or not in extension filter list
	if !ok || (len(exts) > 0 && !inSlice(tag, exts)) {
		return nil, nil
	}
	node = bf.NewNode(bf.CodeBlock)
	node.CodeBlockData = bf.CodeBlockData{
		IsFenced:    true,
		FenceChar:   '`',
		FenceLength: 3,
		Info:        []byte(tag),
	}
	node.Literal, err = ioutil.ReadFile(file)
	if err != nil {
		return nil, errors.Wrapf(err, "read code file %s", file)
	}
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
