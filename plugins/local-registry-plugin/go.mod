module github.com/ideacrafterslabs/ctxt-plugin-local-registry

go 1.26.1

require (
	github.com/ideacrafterslabs/ctxt v0.0.0
	github.com/stretchr/testify v1.11.1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	hop.top/uri v0.0.0-00010101000000-000000000000 // indirect
)

replace (
	github.com/ideacrafterslabs/ctxt => ../..
	hop.top/uri => /Users/jadb/.w/ideacrafterslabs/uri/hops/main
)
