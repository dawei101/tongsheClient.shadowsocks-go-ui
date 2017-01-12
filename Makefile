

generate:
	go-bindata -prefix "res/" res/...
	lessc ui/styles

all:
	go-bindata -prefix "res/" res/...
	CC=clang go build

debug:
	go-bindata -prefix "res/" res/...
	CC=clang go build

clean:
	rm -rf desktop
