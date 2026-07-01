module github.com/ideacrafterslabs/ctxt-plugin-markdown-export

go 1.26.1

require (
	github.com/ideacrafterslabs/ctxt v0.0.0
	github.com/stretchr/testify v1.11.1
	hop.top/cite v0.1.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/ideacrafterslabs/ctxt => ../..
	// hop.top/cite is resolved via the repo-root go.work replace directives.
)
