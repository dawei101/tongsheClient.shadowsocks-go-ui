#!/bin/bash

SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE" ]; do # resolve $SOURCE until the file is no longer a symlink
  DIR="$( cd -P "$( dirname "$SOURCE" )" && pwd )"
  SOURCE="$(readlink "$SOURCE")"
  [[ $SOURCE != /* ]] && SOURCE="$DIR/$SOURCE" # if $SOURCE was a relative symlink, we need to resolve it relative to the path where the symlink file was located
done
DIR="$( cd -P "$( dirname "$SOURCE" )" && pwd )"

cd $DIR

if [ -z "$GOPATH" ];then
    echo "GOPATH is not set, please set first"
    exit 1
fi

export PATH=$PATH:$GOPATH/bin

echo "build tonghse in "$DIR" with GOPATH:" $GOPATH 


echo "get go-bindata..."
go get -u github.com/jteeuwen/go-bindata/...
echo "get go-bindata done..."

echo "bindata:translate all res to bindata.go ..."
./vendor/bin/go-bindata -prefix "res/" res/...
echo "bindata:translate done ..."

echo "download vendor..."
go get ./...
echo "download vendor done..."


mkdir -p bin/


echo "#########################################################"
echo "compile for windows-386..."

GOOS=windows GOARCH=386  go build -o ./bin/tongshe.exe -v .

#################
#echo "compile for linux-386..."
#GOOS=linux GOARCH=386 CGO_ENABLED=1 go build -o ./bin/tongshe.linux.386 -v .
#
#echo "compile for linux-amd64..."
#GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -o ./bin/tongshe.linux.amd64 -v .
#################

echo "#########################################################"
echo "compile for darwin-amd64..."
GOOS=darwin GOARCH=amd64  go build -o ./bin/tongshe.darwin.amd64 -v .

echo "compile done"
echo "the binary files is in "$DIR/bin/
