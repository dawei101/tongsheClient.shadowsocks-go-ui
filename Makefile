mkfile_path := $(abspath $(lastword $(MAKEFILE_LIST)))
current_dir := $(notdir $(patsubst %/,%,$(dir $(mkfile_path))))

GOPATH := ${current_dir}
PATH := ${PATH}:${current_dir}/bin

deps:
	go get ./...

.PHONY : deps
build:
	go get ./...
	go-bindata -prefix "res/" res/...
	CC=clang go build

clean:
	rm -rf desktop
