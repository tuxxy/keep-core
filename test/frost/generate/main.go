package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
)

func main() {
	if len(os.Args) != 3 {
		panic("usage: generate <compiled-frost-directory> <output.go>")
	}
	root := os.Args[1]
	out := os.Args[2]
	names := []string{"FrostWalletRegistry", "FrostDkgValidator"}
	var abis, bins []string
	for _, n := range names {
		raw, e := os.ReadFile(filepath.Join(root, n+".sol", n+".json"))
		if e != nil {
			panic(e)
		}
		var a struct {
			ABI      json.RawMessage `json:"abi"`
			Bytecode string          `json:"bytecode"`
		}
		if e = json.Unmarshal(raw, &a); e != nil {
			panic(e)
		}
		abis = append(abis, string(a.ABI))
		bins = append(bins, a.Bytecode)
	}
	s, e := bind.Bind(names, abis, bins, nil, "frostabi", bind.LangGo, nil, nil)
	if e != nil {
		panic(e)
	}
	if e = os.WriteFile(out, []byte(s), 0644); e != nil {
		panic(e)
	}
}
