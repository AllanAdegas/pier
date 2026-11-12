package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"pier/internal/instance"
)

func runRestore() error {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	file := flags.String("file", "", "Backup criptografado")
	passFile := flags.String("passphrase-file", "", "Arquivo com a frase-senha")
	dir := flags.String("data-dir", "", "Diretório de dados do Pier parado")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if *file == "" || *passFile == "" || *dir == "" || flags.NArg() != 0 {
		return fmt.Errorf("use: pier restore --file X --passphrase-file Y --data-dir D")
	}
	f, err := os.Open(*passFile)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 1026))
	if err != nil {
		return err
	}
	if len(raw) > 1025 {
		return fmt.Errorf("frase-senha muito longa")
	}
	return instance.Restore(context.Background(), *file, strings.TrimRight(string(raw), "\r\n"), *dir)
}
