//go:build ignore

// hash-token prints the bcrypt hash (DefaultCost) of each plaintext token
// argument. Use the output to update seed-db.sh or insert directly into the
// tokens table.
//
// Usage:
//
//	go run scripts/hash-token.go dev-token dev-token-2
package main

import (
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/hash-token.go <token> [token...]")
		os.Exit(1)
	}
	for _, t := range os.Args[1:] {
		hash, err := bcrypt.GenerateFromPassword([]byte(t), bcrypt.DefaultCost)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error hashing %q: %v\n", t, err)
			os.Exit(1)
		}
		fmt.Printf("%s\n", hash)
	}
}
