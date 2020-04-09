export CGO_ENABLED=0
export GO111MODULE=on

ifdef VERSION
STORAGE_CONTROLLER_VERSION := $(VERSION)
else
STORAGE_CONTROLLER_VERSION := latest
endif

.PHONY: build

all: build

build: # @HELP build the source code
build: deps license_check linters
	GOOS=linux GOARCH=amd64 go build -o build/cache-storage-controller/_output/cache-storage-controller ./cmd/cache-storage-controller

deps: # @HELP ensure that the required dependencies are in place
	go build -v ./...
	bash -c "diff -u <(echo -n) <(git diff go.mod)"
	bash -c "diff -u <(echo -n) <(git diff go.sum)"

test: # @HELP run the unit tests and source code validation
test: build license_check linters
	go test github.com/atomix/cache-storage-controller/...

linters: # @HELP examines Go source code and reports coding problems
	GOGC=75  golangci-lint run

license_check: # @HELP examine and ensure license headers exist
	./build/licensing/boilerplate.py -v

images: # @HELP build cache-storage Docker image
images: build
	docker build . -f build/cache-storage-controller/Dockerfile -t atomix/cache-storage-controller:${STORAGE_CONTROLLER_VERSION}

push: # @HELP push cache-storage Docker image
	docker push atomix/cache-storage-controller:${STORAGE_CONTROLLER_VERSION}
