setup:
	GO111MODULE=off go get \
		github.com/Songmu/goxz/cmd/goxz \
		github.com/tcnksm/ghr \
		golang.org/x/lint/golint

test: setup
	go test -v ./...

lint: setup
	go vet ./...
	golint -set_exit_status ./...

.PHONY: setup test lint