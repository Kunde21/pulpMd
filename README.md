# Pulp Markdown  
Code block injection for markdown files.

[![Build Status](https://travis-ci.org/Kunde21/pulpMd.svg?branch=master)](https://travis-ci.org/Kunde21/pulpMd)

## Set snippet tags  
We use the common double-brace format for the snippet tags:  
```
These all snippets with the name, in alphabetical order:

{{snippet <Snippet Name>}}
```

Optionally, you can add a file extension filter to the snippet tag to limit the code to (a) specific language(s).  This will only insert from files named \<Snippet Name>.{js,go,rs}:
```
Prints js, go, rust, in this order:

{{snippet <Snippet Name> [js,go,rs]}}
```

Note: `{{snipped FileName []}}` is different semantic than `{{snipped FileName}}`. 
First prints nothing, second all matches.

## Create snippets  
Snippets are matched to the file name, with the code block tagged with the file extension.  
  
So, if we have the snippet tag:
```md
### Here's an example:
{{snippet SampleCode [js]}}
```
and code file SampleCode.js:
```js
function SampleFunction() {
  console.log("Show in markdown");
}
```

The resulting markdown will be:
````md
### Here's an example:
```js
function SampleFunction() {
  console.log("Show in markdown");
}
```
````
    
## Usage
To inject in-place, simply run: 
```sh
$ pulpMd --target=input.md
```
Specify an output file with:
```sh
$ pulpMd --target=input.md --output=output.md
```

#### Additional options:
```
--injectDir (-d): Root directory of snippets to inject
--norecur   (-r): No recursive searches in injectDir
--output    (-o): Output markdown file
--fileExt   (-e): Extension list.  Can be used to filter "js,go,java" or used to specify the markdown code identifier "aspx:asp".
--notags    (-n): Leave snippet tags in output.  [To facilitate multiple-pass processing]
--quotes    (-q): Leave block quotes in output when no code was inserted in the following tag.  [Default cleans up block-quote headings if there's no code to insert]
--stdin     (-s): Read Stdin for the markdown file to parse.
```
