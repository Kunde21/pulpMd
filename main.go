package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Kunde21/markdownfmt/markdown"
	"github.com/pkg/errors"
	bf "gopkg.in/russross/blackfriday.v2"
)

var codeTags = map[string]string{
	".go":   "go",
	".js":   "js",
	".json": "json",
	".sh":   "shell",
}

func main() {
	regex := regexp.MustCompile(`{{\s*snippet ([^ \t]+)\s*(\[(.*)\])?\s*}}`)
	f, err := ioutil.ReadFile("test.md")
	if err != nil {
		log.Fatal(err)
	}
	render := markdown.NewRenderer(nil)
	md := bf.New(bf.WithExtensions(bf.FencedCode|bf.Tables|bf.HeadingIDs), bf.WithRenderer(render))
	unlinkSet := make([]*bf.Node, 0)
	nodes := md.Parse(f)
	nodes.Walk(func(n *bf.Node, entering bool) bf.WalkStatus {
		// snippet tags will only be in text nodes
		if !entering || n.Type != bf.Text ||
			!regex.Match(n.Literal) ||
			n.Parent.Type != bf.Paragraph ||
			n.Parent.Parent.Type != bf.Document {
			return bf.GoToNext
		}

		strs := regex.FindSubmatch(n.Literal)
		mth := strs[1]
		exts := strings.Split(string(strs[3]), ",")
		if len(mth) < 2 {
			return bf.GoToNext
		}
		pattern := fmt.Sprintf("%s.*", mth)
		matches, err := filepath.Glob(pattern)
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
		// Remove leading blockquote if no code was added.
		if count == 0 && n.Parent.Prev.Type == bf.BlockQuote {
			unlinkSet = append(unlinkSet, n.Parent.Prev)
		}
		unlinkSet = append(unlinkSet, n.Parent)
		return bf.GoToNext
	})
	for _, n := range unlinkSet {
		n.Unlink()
	}
	out, err := os.Create("output.md")
	if err != nil {
		log.Fatal(err)
	}
	nodes.Walk(func(n *bf.Node, entering bool) bf.WalkStatus {
		return render.RenderNode(out, n, entering)
	})
	out.Close()
}

func codeNode(file string, exts []string) (node *bf.Node, err error) {
	tag, ok := codeTags[filepath.Ext(file)]
	if !ok ||
		(len(exts) > 0 && !inSlice(tag, exts)) {
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
