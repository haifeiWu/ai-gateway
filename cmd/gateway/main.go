package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/ai-gateway/internal/config"
	"github.com/ai-gateway/internal/model"
	"github.com/ai-gateway/internal/repository"
	"github.com/ai-gateway/internal/router"
	"github.com/ai-gateway/internal/service"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// 数据库连接
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBName)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
	})
	if err != nil {
		slog.Error("failed to connect database", "error", err)
		os.Exit(1)
	}

	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)

	// 自动迁移
	if err := db.AutoMigrate(&model.Tenant{}, &model.APIKey{}, &model.UsageRecord{}); err != nil {
		slog.Error("failed to auto migrate", "error", err)
		os.Exit(1)
	}

	// 用量异步写入器
	usageWriter := service.NewUsageWriter(repository.NewUsageStore(db))

	// 路由
	r := router.Setup(db, cfg, usageWriter)

	// 优雅关闭
	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: r,
	}

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		slog.Info("shutting down server...")
		usageWriter.Shutdown()
		sqlDB.Close()
	}()

	slog.Info("gateway starting", "addr", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
