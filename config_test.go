package main

import (
	"fmt"
	"testing"
)

func TestStorageDir(t *testing.T) {
	d := GetStorageDir()
	fmt.Println("Storage dir is:", d)
	if !isPathExist(d) {
		t.Errorf("Storage path is not created, :: %s", d)
	}
}
