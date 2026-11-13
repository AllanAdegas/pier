package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"pier/internal/identity"
	"pier/internal/instance"
	"pier/internal/platform/sqlite"
	"strings"
	"time"
)

// runAdmin is an offline recovery path for the operator holding the data directory.
// Passwords are read from a file, never the command line or process environment.
func runAdmin() error {
	if len(os.Args) < 3 {
		return fmt.Errorf("use: pier admin reset-password|disable-2fa --data-dir D --email E [--password-file F]")
	}
	action := os.Args[2]
	flags := flag.NewFlagSet("admin", flag.ContinueOnError)
	dir := flags.String("data-dir", "", "Diretório de dados do Pier parado")
	email := flags.String("email", "", "E-mail da conta")
	passwordFile := flags.String("password-file", "", "Arquivo com a nova senha")
	if err := flags.Parse(os.Args[3:]); err != nil {
		return err
	}
	if *dir == "" || *email == "" || flags.NArg() != 0 || (action != "reset-password" && action != "disable-2fa") {
		return fmt.Errorf("informe ação, --data-dir e --email")
	}
	if _, err := os.Stat(filepath.Join(*dir, "pier.db")); err != nil {
		return err
	}
	unlock, err := instance.Lock(*dir)
	if err != nil {
		return err
	}
	defer unlock()
	db, err := sqlite.Open(context.Background(), filepath.Join(*dir, "pier.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	service := identity.NewService(identity.NewStore(db), 14*24*time.Hour)
	if action == "disable-2fa" {
		return service.RemoveTwoFactorByEmail(context.Background(), *email)
	}
	if *passwordFile == "" {
		return fmt.Errorf("informe --password-file")
	}
	stat, err := os.Stat(*passwordFile)
	if err != nil {
		return err
	}
	if stat.Size() > 1024 {
		return fmt.Errorf("arquivo de senha muito grande")
	}
	raw, err := os.ReadFile(*passwordFile)
	if err != nil {
		return err
	}
	return service.SetPasswordByEmail(context.Background(), *email, strings.TrimRight(string(raw), "\r\n"))
}
