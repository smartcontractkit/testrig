# Native Go `testrig` Hooks

If you prefer to define your test lifecycle hooks in native Go, testrig lets you do so in a non-invasive way by creating your own test execution binary.

1. Setup your test binary as a new go module inside your project.
   ```sh
   mkdir tools/test && cd tools/test/
   go mod init github.com/yourOrg/yourProject/tools/test
   touch main.go
   ```
2. Setup your `main.go` to register test lifecycle hooks. See [example](./main.go)
3. (Optional) Add `tool github.com/yourOrg/yourProject/tools/test` to your main `go.mod`. This is so you can easily call your test setup with `go tool test run|diagnose`. If you prefer not to, or hit dependency issues because of this, you can use `go -C tools/test run|diagnose` instead.
