package singleton

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"github.com/naiba/nezha/model"
	"github.com/naiba/nezha/pkg/utils"
	pb "github.com/naiba/nezha/proto"
)

var Version = "v0.12.18" // ！！记得修改 README 中的 badge 版本！！

var (
	Conf  *model.Config
	Cache *cache.Cache
	DB    *gorm.DB
	Loc   *time.Location

	ServerList map[uint64]*model.Server // [ServerID] -> model.Server
	SecretToID map[string]uint64        // [ServerSecret] -> ServerID
	ServerLock sync.RWMutex

	SortedServerList []*model.Server // 用于存储服务器列表的 slice，按照服务器 ID 排序
	SortedServerLock sync.RWMutex
)

// Init 初始化时区为上海时区
func Init() {
	var err error
	Loc, err = time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
}

// ReSortServer 根据服务器ID 对服务器列表进行排序（ID越大越靠前）
func ReSortServer() {
	ServerLock.RLock()
	defer ServerLock.RUnlock()
	SortedServerLock.Lock()
	defer SortedServerLock.Unlock()

	SortedServerList = []*model.Server{}
	for _, s := range ServerList {
		SortedServerList = append(SortedServerList, s)
	}

	// 按照服务器 ID 排序的具体实现（ID越大越靠前）
	sort.SliceStable(SortedServerList, func(i, j int) bool {
		if SortedServerList[i].DisplayIndex == SortedServerList[j].DisplayIndex {
			return SortedServerList[i].ID < SortedServerList[j].ID
		}
		return SortedServerList[i].DisplayIndex > SortedServerList[j].DisplayIndex
	})
}

// =============== Cron Mixin ===============

var CronLock sync.RWMutex
var Crons map[uint64]*model.Cron
var Cron *cron.Cron

func ManualTrigger(c model.Cron) {
	CronTrigger(c)()
}

func CronTrigger(cr model.Cron) func() {
	crIgnoreMap := make(map[uint64]bool)
	for j := 0; j < len(cr.Servers); j++ {
		crIgnoreMap[cr.Servers[j]] = true
	}
	return func() {
		ServerLock.RLock()
		defer ServerLock.RUnlock()
		for _, s := range ServerList {
			if cr.Cover == model.CronCoverAll && crIgnoreMap[s.ID] {
				continue
			}
			if cr.Cover == model.CronCoverIgnoreAll && !crIgnoreMap[s.ID] {
				continue
			}
			if s.TaskStream != nil {
				s.TaskStream.Send(&pb.Task{
					Id:   cr.ID,
					Data: cr.Command,
					Type: model.TaskTypeCommand,
				})
			} else {
				SendNotification(fmt.Sprintf("[任务失败] %s，服务器 %s 离线，无法执行。", cr.Name, s.Name), false)
			}
		}
	}
}

func IPDesensitize(ip string) string {
	if Conf.EnablePlainIPInNotification {
		return ip
	}
	return utils.IPDesensitize(ip)
}
