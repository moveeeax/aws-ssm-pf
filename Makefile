.PHONY: build test lint fmt install clean

build:
	go build -o ssm-pf .

test:
	go test -race ./...

fmt:
	gofmt -w .

lint:
	gofmt -l . && go vet ./...

# `go install .` would name the binary aws-ssm-pf (after the module path);
# build it as ssm-pf into the directory go install would use instead.
install:
	go build -o "$$(d=$$(go env GOBIN); [ -n "$$d" ] || d=$$(go env GOPATH | cut -d: -f1)/bin; echo $$d)/ssm-pf" .

clean:
	rm -f ssm-pf
