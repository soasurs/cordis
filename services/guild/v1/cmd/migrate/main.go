package main

import (
	"context"
	"flag"

	"github.com/zeromicro/go-zero/core/conf"

	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
	"github.com/soasurs/cordis/services/guild/v1/config"
	guildmigrations "github.com/soasurs/cordis/services/guild/v1/db/migrations"
)

var configPath = flag.String("c", "etc/config.yaml", "config file of service")

func main() {
	flag.Parse()

	cfg := new(config.Config)
	if err := conf.LoadConfig(*configPath, cfg, conf.UseEnv()); err != nil {
		panic(err)
	}
	db, err := database.NewPostgres(cfg.Database)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	if err := migration.Apply(context.Background(), db, guildmigrations.Files); err != nil {
		panic(err)
	}
}
