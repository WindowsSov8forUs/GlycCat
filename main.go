package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/WindowsSov8forUs/glyccat/config"
	"github.com/WindowsSov8forUs/glyccat/database"
	"github.com/WindowsSov8forUs/glyccat/fileserver"
	"github.com/WindowsSov8forUs/glyccat/log"
	"github.com/WindowsSov8forUs/glyccat/processor"
	"github.com/WindowsSov8forUs/glyccat/server"
	"github.com/WindowsSov8forUs/glyccat/sys"
	"github.com/WindowsSov8forUs/glyccat/version"

	"github.com/gin-gonic/gin"
	"github.com/tencent-connect/botgo"
)

func main() {
	// 定义 faststart 命令行标志，默认为 false
	fastStart := flag.Bool("faststart", false, "是否快速启动")

	// 定义 debug 命令行标志，默认为 false
	debug := flag.Bool("debug", false, "是否启用调试模式")

	// 解析命令行参数到定义的标志
	flag.Parse()

	// 检查是否使用了 -faststart 参数
	if !*fastStart {
		sys.InitBase()
	}

	fmt.Println(version.Logo())
	versionString := log.StringCenter(fmt.Sprintf("GlycCat %s", version.Version), 58)
	log.PrintlnCyan(versionString)
	fmt.Print("\n==========================================================\n\n")

	// 加载配置
	conf, err := config.LoadConfig("config.yml")
	if err != nil {
		fmt.Printf("%s 加载配置文件时出错: %v\n", log.FailMark, log.Red(fmt.Sprint(err)))
		os.Exit(0)
		return
	}

	// 配置日志等级
	log.SetLogLevel(conf.LogLevel)

	// 设置 gin 运行模式
	if *debug {
		log.Warn("正在 Debug 模式下运行服务器！")
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// 设置 logger
	logger := log.GetLogger()
	botgo.SetLogger(logger)

	// 如果配置并未设置
	if conf.Account.Token == "" {
		log.Fatal("检测到未完成机器人配置，请修改配置文件后重启程序")
		os.Exit(0)
		return
	}

	// 开启本地文件服务器
	fileserver.StartFileServer(conf)

	// 启动消息数据库
	if conf.Database.MessageDatabase.Enable {
		log.Info("正在启动消息数据库...")
		err := database.StartMessageDB(conf.Database.MessageDatabase.Limit)
		if err != nil {
			log.Errorf("启动消息数据库时出错，将无法使用消息缓存: %v", err)
		}
	} else {
		log.Warn("消息数据库未启动，将无法使用消息缓存。")
	}

	// 初始化消息处理器
	p, ctx, err := processor.NewProcessor(conf)
	if err != nil {
		log.Fatalf("建立与 QQ 开放平台连接时出错: %v", err)
	}
	// 创建 Satori 服务端
	server, err := server.NewServer(p.Api, p.ApiV2, conf)
	if err != nil {
		log.Fatalf("建立 Satori 服务端时出错: %v", err)
	}
	// 运行消息处理器
	err = p.Run(ctx, server)
	if err != nil {
		log.Fatalf("应用启动时出错: %v", err)
	}

	// 使用通道来等待信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 等待信号
	<-sigCh

	server.Close()
}
