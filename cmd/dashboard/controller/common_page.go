package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/hashicorp/go-uuid"
	"github.com/jinzhu/copier"

	"github.com/naiba/nezha/model"
	"github.com/naiba/nezha/pkg/utils"
	"github.com/naiba/nezha/pkg/websocketx"
	"github.com/naiba/nezha/proto"
	"github.com/naiba/nezha/service/rpc"
	"github.com/naiba/nezha/service/singleton"
)

type commonPage struct {
	r *gin.Engine
}

func (cp *commonPage) serve() {
	cr := cp.r.Group("")
	cr.GET("/service", cp.service)
	// TODO: 界面直接跳转使用该接口
	cr.GET("/network/:id", cp.network)
	cr.GET("/network", cp.network)
	cr.GET("/file", cp.createFM)
	cr.GET("/file/:id", cp.fm)
}

func (p *commonPage) service(c *gin.Context) {
	res, _, _ := requestGroup.Do("servicePage", func() (interface{}, error) {
		singleton.AlertsLock.RLock()
		defer singleton.AlertsLock.RUnlock()
		var stats map[uint64]model.ServiceItemResponse
		var statsStore map[uint64]model.CycleTransferStats
		copier.Copy(&stats, singleton.ServiceSentinelShared.LoadStats())
		copier.Copy(&statsStore, singleton.AlertsCycleTransferStatsStore)
		for k, service := range stats {
			if !service.Monitor.EnableShowInService {
				delete(stats, k)
			}
		}
		return []interface {
		}{
			stats, statsStore,
		}, nil
	})
	c.HTML(http.StatusOK, "", gin.H{
		// "Title":              singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ServicesStatus"}),
		"Services":           res.([]interface{})[0],
		"CycleTransferStats": res.([]interface{})[1],
	})
}

func (cp *commonPage) network(c *gin.Context) {
	var (
		monitorHistory       *model.MonitorHistory
		servers              []model.Server
		serverIdsWithMonitor []uint64
		monitorInfos         = []byte("{}")
		id                   uint64
	)
	if len(singleton.SortedServerList) > 0 {
		id = singleton.SortedServerList[0].ID
	}
	if err := singleton.DB.Model(&model.MonitorHistory{}).Select("monitor_id, server_id").
		Where("monitor_id != 0 and server_id != 0").Limit(1).First(&monitorHistory).Error; err != nil {
		// mygin.ShowErrorPage(c, mygin.ErrInfo{
		// 	Code:  http.StatusForbidden,
		// 	Title: "请求失败",
		// 	Msg:   "请求参数有误：" + "server monitor history not found",
		// 	Link:  "/",
		// 	Btn:   "返回重试",
		// }, true)
		return
	} else {
		if monitorHistory == nil || monitorHistory.ServerID == 0 {
			if len(singleton.SortedServerList) > 0 {
				id = singleton.SortedServerList[0].ID
			}
		} else {
			id = monitorHistory.ServerID
		}
	}

	idStr := c.Param("id")
	if idStr != "" {
		var err error
		id, err = strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			// mygin.ShowErrorPage(c, mygin.ErrInfo{
			// 	Code:  http.StatusForbidden,
			// 	Title: "请求失败",
			// 	Msg:   "请求参数有误：" + err.Error(),
			// 	Link:  "/",
			// 	Btn:   "返回重试",
			// }, true)
			return
		}
		_, ok := singleton.ServerList[id]
		if !ok {
			// mygin.ShowErrorPage(c, mygin.ErrInfo{
			// 	Code:  http.StatusForbidden,
			// 	Title: "请求失败",
			// 	Msg:   "请求参数有误：" + "server id not found",
			// 	Link:  "/",
			// 	Btn:   "返回重试",
			// }, true)
			return
		}
	}
	monitorHistories := singleton.MonitorAPI.GetMonitorHistories(map[string]any{"server_id": id})
	monitorInfos, _ = utils.Json.Marshal(monitorHistories)
	_, isMember := c.Get(model.CtxKeyAuthorizedUser)
	var isViewPasswordVerfied bool

	if err := singleton.DB.Model(&model.MonitorHistory{}).
		Select("distinct(server_id)").
		Where("server_id != 0").
		Find(&serverIdsWithMonitor).
		Error; err != nil {
		// mygin.ShowErrorPage(c, mygin.ErrInfo{
		// 	Code:  http.StatusForbidden,
		// 	Title: "请求失败",
		// 	Msg:   "请求参数有误：" + "no server with monitor histories",
		// 	Link:  "/",
		// 	Btn:   "返回重试",
		// }, true)
		return
	}
	if isMember || isViewPasswordVerfied {
		for _, server := range singleton.SortedServerList {
			for _, id := range serverIdsWithMonitor {
				if server.ID == id {
					servers = append(servers, *server)
				}
			}
		}
	} else {
		for _, server := range singleton.SortedServerListForGuest {
			for _, id := range serverIdsWithMonitor {
				if server.ID == id {
					servers = append(servers, *server)
				}
			}
		}
	}
	serversBytes, _ := utils.Json.Marshal(model.StreamServerData{
		Now: time.Now().Unix() * 1000,
		// Servers: servers,
	})

	c.HTML(http.StatusOK, "", gin.H{
		"Servers":      string(serversBytes),
		"MonitorInfos": string(monitorInfos),
	})
}

func (cp *commonPage) fm(c *gin.Context) {
	streamId := c.Param("id")
	if _, err := rpc.NezhaHandlerSingleton.GetStream(streamId); err != nil {
		// mygin.ShowErrorPage(c, mygin.ErrInfo{
		// 	Code:  http.StatusForbidden,
		// 	Title: "无权访问",
		// 	Msg:   "FM会话不存在",
		// 	Link:  "/",
		// 	Btn:   "返回首页",
		// }, true)
		return
	}
	defer rpc.NezhaHandlerSingleton.CloseStream(streamId)

	wsConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// mygin.ShowErrorPage(c, mygin.ErrInfo{
		// 	Code: http.StatusInternalServerError,
		// 	// Title: singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{
		// 	// 	MessageID: "NetworkError",
		// 	// }),
		// 	Msg:  "Websocket协议切换失败",
		// 	Link: "/",
		// 	Btn:  "返回首页",
		// }, true)
		return
	}
	defer wsConn.Close()
	conn := websocketx.NewConn(wsConn)

	go func() {
		// PING 保活
		for {
			if err = conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
			time.Sleep(time.Second * 10)
		}
	}()

	if err = rpc.NezhaHandlerSingleton.UserConnected(streamId, conn); err != nil {
		return
	}

	rpc.NezhaHandlerSingleton.StartStream(streamId, time.Second*10)
}

func (cp *commonPage) createFM(c *gin.Context) {
	IdString := c.Query("id")
	if _, authorized := c.Get(model.CtxKeyAuthorizedUser); !authorized {
		// mygin.ShowErrorPage(c, mygin.ErrInfo{
		// 	Code:  http.StatusForbidden,
		// 	Title: "无权访问",
		// 	Msg:   "用户未登录",
		// 	Link:  "/login",
		// 	Btn:   "去登录",
		// }, true)
		return
	}

	streamId, err := uuid.GenerateUUID()
	if err != nil {
		// mygin.ShowErrorPage(c, mygin.ErrInfo{
		// 	Code: http.StatusInternalServerError,
		// 	// Title: singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{
		// 	// 	MessageID: "SystemError",
		// 	// }),
		// 	Msg:  "生成会话ID失败",
		// 	Link: "/server",
		// 	Btn:  "返回重试",
		// }, true)
		return
	}

	rpc.NezhaHandlerSingleton.CreateStream(streamId)

	serverId, err := strconv.Atoi(IdString)
	if err != nil {
		// mygin.ShowErrorPage(c, mygin.ErrInfo{
		// 	Code:  http.StatusForbidden,
		// 	Title: "请求失败",
		// 	Msg:   "请求参数有误：" + err.Error(),
		// 	Link:  "/server",
		// 	Btn:   "返回重试",
		// }, true)
		return
	}

	singleton.ServerLock.RLock()
	server := singleton.ServerList[uint64(serverId)]
	singleton.ServerLock.RUnlock()
	if server == nil {
		// mygin.ShowErrorPage(c, mygin.ErrInfo{
		// 	Code:  http.StatusForbidden,
		// 	Title: "请求失败",
		// 	Msg:   "服务器不存在或处于离线状态",
		// 	Link:  "/server",
		// 	Btn:   "返回重试",
		// }, true)
		return
	}

	fmData, _ := utils.Json.Marshal(&model.TaskFM{
		StreamID: streamId,
	})
	if err := server.TaskStream.Send(&proto.Task{
		Type: model.TaskTypeFM,
		Data: string(fmData),
	}); err != nil {
		// mygin.ShowErrorPage(c, mygin.ErrInfo{
		// 	Code:  http.StatusForbidden,
		// 	Title: "请求失败",
		// 	Msg:   "Agent信令下发失败",
		// 	Link:  "/server",
		// 	Btn:   "返回重试",
		// }, true)
		return
	}

	c.HTML(http.StatusOK, "dashboard-", gin.H{
		"SessionID": streamId,
	})
}
