package fail

fail

/*
This is a non-compiling file that has been added to explicitly ensure that CI fails.
It also contains the command that caused the failure and its output.
Remove this file if debugging locally.

go mod operation failed. This may mean that there are legitimate dependency issues with the "go.mod" definition in the repository and the updates performed by the gomod check. This branch can be cloned locally to debug the issue.

Command that caused error:
./godelw verify --skip-test --skip-check

Output:
Running format...
Running mod...
Running generate...
gateway-client/v2/generate.go
go tool oapi-codegen -config cfg-v2.yaml api-v2.yml
# github.com/vmware-labs/yaml-jsonpath/pkg/yamlpath
/repo/vendor/github.com/vmware-labs/yaml-jsonpath/pkg/yamlpath/path.go:29:9: cannot use p.f(node, root).ToArray() (value of type []*"go.yaml.in/yaml/v4".Node) as []*"gopkg.in/yaml.v3".Node value in return statement
/repo/vendor/github.com/vmware-labs/yaml-jsonpath/pkg/yamlpath/path.go:57:32: cannot use node (variable of type *"gopkg.in/yaml.v3".Node) as *"go.yaml.in/yaml/v4".Node value in argument to yit.FromNode
/repo/vendor/github.com/vmware-labs/yaml-jsonpath/pkg/yamlpath/path.go:70:33: cannot use node (variable of type *"gopkg.in/yaml.v3".Node) as *"go.yaml.in/yaml/v4".Node value in argument to yit.FromNode
/repo/vendor/github.com/vmware-labs/yaml-jsonpath/pkg/yamlpath/path.go:75:33: cannot use node (variable of type *"gopkg.in/yaml.v3".Node) as *"go.yaml.in/yaml/v4".Node value in argument to yit.FromNode
/repo/vendor/github.com/vmware-labs/yaml-jsonpath/pkg/yamlpath/path.go:80:33: cannot use node (variable of type *"gopkg.in/yaml.v3".Node) as *"go.yaml.in/yaml/v4".Node value in argument to yit.FromNode
/repo/vendor/github.com/vmware-labs/yaml-jsonpath/pkg/yamlpath/path.go:190:22: cannot use node (variable of type *"gopkg.in/yaml.v3".Node) as *"go.yaml.in/yaml/v4".Node value in argument to yit.FromNode
/repo/vendor/github.com/vmware-labs/yaml-jsonpath/pkg/yamlpath/path.go:200:25: cannot use a (variable of type *"go.yaml.in/yaml/v4".Node) as *"gopkg.in/yaml.v3".Node value in argument to p.f
/repo/vendor/github.com/vmware-labs/yaml-jsonpath/pkg/yamlpath/path.go:218:33: cannot use node.Content[i] (variable of type *"gopkg.in/yaml.v3".Node) as *"go.yaml.in/yaml/v4".Node value in argument to yit.FromNode
/repo/vendor/github.com/vmware-labs/yaml-jsonpath/pkg/yamlpath/path.go:236:37: cannot use node.Content[i] (variable of type *"gopkg.in/yaml.v3".Node) as *"go.yaml.in/yaml/v4".Node value in argument to yit.FromNode
/repo/vendor/github.com/vmware-labs/yaml-jsonpath/pkg/yamlpath/path.go:252:44: cannot use n (variable of type *"gopkg.in/yaml.v3".Node) as *"go.yaml.in/yaml/v4".Node value in argument to yit.FromNode
/repo/vendor/github.com/vmware-labs/yaml-jsonpath/pkg/yamlpath/path.go:252:44: too many errors
gateway-client/v2/generate.go:17: running "go": exit status 1
generate.go:17: running "go": exit status 1
Error: failed to run go generate in "/repo": exit status 1
Running license...
Failed tasks:
	generate

*/
