package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"

	"github.com/Kunde21/markdownfmt/markdown"
	bf "gopkg.in/russross/blackfriday.v2"
)

var codeTags = map[string]string{
	".go":   "go",
	".js":   "js",
	".json": "json",
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
		fmt.Println(n, entering)
		// snippet tags will only be in text nodes
		if !entering || n.Type != bf.Text ||
			!regex.Match(n.Literal) ||
			n.Parent.Type != bf.Paragraph ||
			n.Parent.Parent.Type != bf.Document {
			return bf.GoToNext
		}

		strs := regex.FindSubmatch(n.Literal)
		mth := strs[1]
		exts := bytes.Split(strs[3], []byte{','})
		fmt.Println(exts)
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
			tag, ok := codeTags[filepath.Ext(v)]
			if !ok ||
				(len(exts) > 0 && !inSlice([]byte(tag), exts)) {
				continue
			}
			codeNode := bf.NewNode(bf.CodeBlock)
			codeNode.CodeBlockData = bf.CodeBlockData{
				IsFenced:    true,
				FenceChar:   '`',
				FenceLength: 3,
				Info:        []byte(tag),
			}
			codeNode.Literal, err = ioutil.ReadFile(v)
			if err != nil {
				log.Fatal(err)
				continue
			}
			n.Parent.InsertBefore(codeNode)
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

func inSlice(needle []byte, haystack [][]byte) bool {
	for _, v := range haystack {
		if bytes.Equal(needle, bytes.TrimSpace(v)) {
			return true
		}
	}
	return false
}
