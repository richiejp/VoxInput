# Configuration

- Access environment variables and command line arguments in main.go

# Style

- Use the linux kernel guidelines for commenting insofar as they are applicable to Go (e.g. avoid stating the obvious)
- Use `any` instead of `interface{}` and in general use modern Go

# Error handling

- Wrap errors when passing them back up the stack
