package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"wechatdll/TcpPoll"
	"wechatdll/comm"
	_ "wechatdll/routers"
	"wechatdll/srv/wxcore"

	"github.com/astaxie/beego"
	webcontext "github.com/astaxie/beego/context"
	"github.com/lunny/log"
)

func main() {
	managed := os.Getenv("ROBOT_PROTOCOL_MANAGED") == "1"
	if managed {
		_ = beego.AppConfig.Set("syncmessage", "false")
		_ = beego.AppConfig.Set("longlinkenabled", "false")
		port, err := strconv.Atoi(os.Getenv("ROBOT_PROTOCOL_PORT"))
		if err != nil || port < 1 || port > 65535 {
			panic("invalid protocol port")
		}
		beego.BConfig.Listen.HTTPAddr = "127.0.0.1"
		beego.BConfig.Listen.HTTPPort = port
		beego.InsertFilter("/*", beego.BeforeRouter, func(ctx *webcontext.Context) {
			if ctx.Input.Header("Authorization") != "Bearer "+os.Getenv("ROBOT_PROTOCOL_TOKEN") {
				ctx.Output.SetStatus(401)
				ctx.Output.Body([]byte("unauthorized"))
			}
		})
	}
	beego.Get("/robot/health", func(ctx *webcontext.Context) {
		ctx.Output.JSON(map[string]interface{}{"instance": os.Getenv("ROBOT_PROTOCOL_INSTANCE"), "service": "wechat", "pid": os.Getpid()}, false, false)
	})
	beego.Get("/robot/account", func(ctx *webcontext.Context) {
		if err := comm.RedisClient.Ping().Err(); err != nil {
			ctx.Output.SetStatus(503)
			ctx.Output.Body([]byte("account store unavailable"))
			return
		}
		account, _ := comm.GetLoginata(ctx.Input.Query("wxid"), nil)
		status := "expired"
		if account != nil && account.Wxid != "" {
			status = "alive"
		}
		ctx.Output.JSON(map[string]interface{}{"code": 0, "success": true, "data": map[string]string{"status": status}}, false, false)
	})
	longLinkEnabled, _ := beego.AppConfig.Bool("longlinkenabled")

	comm.RedisInitialize()
	_, err := comm.RedisClient.Ping().Result()
	if err != nil {
		panic(fmt.Sprintf("【Redis】连接失败，ERROR：%v", err.Error()))
	}

	comm.RabbitMQSetup()

	sysType := runtime.GOOS

	if sysType == "linux" && longLinkEnabled {
		// LINUX系统
		tcpManager, err := TcpPoll.GetTcpManager()
		if err != nil {
			log.Errorf("TCP启动失败.")
		}
		go tcpManager.RunEventLoop()
	}

	// bee generate docs 生成文档

	// 初始化自动心跳包
	if !managed {
		go wxcore.GetWXConnectMgr().InitAutoHeartBeat()
	}

	// 初始化验证地址
	if managed {
		comm.InitProxy("0")
	} else {
		comm.InitProxy("8888")
	}
	//
	// 启动
	if err := comm.GetProxy().Start(); err != nil {
		log.Fatal(err)
	}

	beego.BConfig.WebConfig.DirectoryIndex = true
	beego.BConfig.WebConfig.StaticDir["/"] = "swagger"

	beego.SetLogFuncCall(false)
	//beego.InsertFilter("/*", beego.BeforeRouter, middleware.BaseAuthLog, false)
	beego.Run()
	// 生成 swagger 文档 bee generate docs
}

//func GetA16Data(DevicelId string) string {
//	return "A" + DevicelId[1:16]
//}
//
//func main() {
//	fmt.Println(GetA16Data(("feuiwheuifhweuihf")))
//}
