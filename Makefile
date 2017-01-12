

generate:
	go-bindata -prefix "res/" res/...

build:
	go-bindata -prefix "res/" res/...
	CC=clang go build

clean:
	rm -rf desktop
