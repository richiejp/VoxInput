
.PHONY: test
test:
	bash each.sh go test -race -v ./...

.PHONY: cov
cov:
	bash each.sh go test -race -covermode=atomic -coverprofile=coverage.out ./...

.PHONY: tidy
tidy:
	bash each.sh go mod tidy

.PHONY: lint
lint:
	bash each.sh golangci-lint run
