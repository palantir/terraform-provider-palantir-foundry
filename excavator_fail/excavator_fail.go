package fail

fail

/*
This is a non-compiling file that has been added to explicitly ensure that CI fails.
It also contains the command that caused the failure and its output.
Remove this file if debugging locally.

go mod operation failed. This may mean that there are legitimate dependency issues with the "go.mod" definition in the repository and the updates performed by the gomod check. This branch can be cloned locally to debug the issue.

Command that caused error:
./godelw exec -- go get github.com/imdario/mergo

Output:
go: github.com/imdario/mergo@upgrade (v1.0.2) requires github.com/imdario/mergo@v1.0.2: parsing go.mod:
	module declares its path as: dario.cat/mergo
	        but was required as: github.com/imdario/mergo
Error: exit status 1

*/
