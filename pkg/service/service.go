package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/angch/gogcbot/pkg/bot"
	"github.com/angch/gogcbot/pkg/config"
	"github.com/angch/gogcbot/pkg/db"
	"github.com/kardianos/service"
)

type BotProgram struct {
	CfgFile string
	cancel  context.CancelFunc
	bot     *bot.Bot
	db      *db.DB
}

func NewBotProgram(cfgFile string) *BotProgram {
	return &BotProgram{CfgFile: cfgFile}
}

func (p *BotProgram) Start(s service.Service) error {
	log.Println("[Service] Starting GoGCBot service program...")
	go p.run()
	return nil
}

func (p *BotProgram) run() {
	cfg, err := config.LoadConfig(p.CfgFile)
	if err != nil {
		log.Fatalf("[Service] Failed to load config (%s): %v", p.CfgFile, err)
	}

	dbPath := cfg.DBPath
	if !filepath.IsAbs(dbPath) {
		dbPath = filepath.Join(filepath.Dir(p.CfgFile), dbPath)
	}

	database, err := db.OpenDB(dbPath)
	if err != nil {
		log.Fatalf("[Service] Failed to open database (%s): %v", dbPath, err)
	}
	p.db = database

	if err := database.AutoMigrate(); err != nil {
		log.Fatalf("[Service] Database auto-migration failed (%s): %v", dbPath, err)
	}

	b, err := bot.NewBot(cfg, database)
	if err != nil {
		log.Fatalf("[Service] Failed to initialize bot: %v", err)
	}
	b.SetCfgPath(p.CfgFile)
	p.bot = b

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	if err := b.Start(ctx); err != nil {
		log.Printf("[Service] Bot engine stopped: %v", err)
	}
}

func (p *BotProgram) Stop(s service.Service) error {
	log.Println("[Service] Stopping GoGCBot service program...")
	if p.cancel != nil {
		p.cancel()
	}
	if p.db != nil {
		p.db.Close()
	}
	return nil
}

func GetService(cfgFile string) (service.Service, error) {
	absCfg, err := filepath.Abs(cfgFile)
	if err != nil {
		absCfg = cfgFile
	}

	execPath, err := os.Executable()
	if err != nil {
		execPath = ""
	} else {
		if absExec, err := filepath.Abs(execPath); err == nil {
			execPath = absExec
		}
	}

	prg := NewBotProgram(absCfg)
	svcConfig := &service.Config{
		Name:        "GoGCBot",
		DisplayName: "GoGCBot Telegram Moderation Service",
		Description: "Telegram Group Moderation & User Reputation Bot Daemon Service",
		Executable:  execPath,
		Arguments:   []string{"run", "--config", absCfg},
		Option: service.KeyValue{
			"WorkingDirectory": filepath.Dir(absCfg),
		},
	}

	svc, err := service.New(prg, svcConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize OS service manager: %w", err)
	}

	return svc, nil
}
