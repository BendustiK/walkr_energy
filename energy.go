package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	goerrors "github.com/go-errors/errors"
	goredis "gopkg.in/redis.v2"

	"github.com/op/go-logging"
)

var RoundDuration = 10 * time.Minute
var config PlayerInfos
var log = logging.MustGetLogger("Walkr")
var format = logging.MustStringFormatter(
	"%{color}%{time:15:04:05.000} %{shortfile} ▶ %{level:.4s} %{id:03x}%{color:reset} %{message}",
)
var redis *goredis.Client

var redisConf = &goredis.Options{
	Network:      "tcp",
	Addr:         "localhost:6379",
	Password:     "",
	DB:           0,
	DialTimeout:  5 * time.Second,
	ReadTimeout:  5 * time.Second,
	WriteTimeout: 5 * time.Second,
	PoolSize:     20,
	IdleTimeout:  60 * time.Second,
}

type PlayerInfo struct {
	Name            string `json:"-"`
	AuthToken       string `json:"auth_token"`
	ClientVersion   string `json:"client_version"`
	Platform        string `json:"platform"`
	Locale          string `json:"-"`
	Cookie          string `json:"-"`
	ConvertedEnergy int    `json:"converted_energy,string"`
	EpicHelper      bool   `json:"-"`
}

type PlayerInfos struct {
	PlayerInfo []PlayerInfo
}

type BoolResponse struct {
	Success bool
}

type ConfirmFriendRequest struct {
	AuthToken     string `json:"auth_token"`
	UserId        int    `json:"user_id"`
	ClientVersion string `json:"client_version"`
	Platform      string `json:"platform"`
}

type NewFriendListResponse struct {
	Data []Friend `json:"data"`
}
type Friend struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

func MakeRequest(playerInfo PlayerInfo, ch chan int) {
	for {
		select {
		case <-ch:
			fmt.Println("exiting...")
			ch <- 1
			break
		default:
		}

		defer func() {
			if r := recover(); r != nil {
				msg := goerrors.Wrap(r, 2).ErrorStack()
				log.Error("程序挂了: %v", msg)
				return
			}
		}()
		// log.Notice("===================== 第%v轮 =====================", curRound)
		// _checkFriendInvitation(playerInfo)

		_convertEnegeryToPilots(playerInfo)

		_incrRound(playerInfo)
		time.Sleep(RoundDuration)
	}

}
func _convertEnegeryToPilots(playerInfo PlayerInfo) bool {
	playerInfo = _generateEnergy(playerInfo)

	b, err := json.Marshal(playerInfo)
	if err != nil {
		log.Error("转换「%v」的数据格式错误: %v", playerInfo.Name, err)
		return false
	}

	client := &http.Client{}

	host := "https://universe.walkrgame.com/api/v1/pilots/convert"
	req, err := _generateRequest(playerInfo, host, "POST", bytes.NewBuffer([]byte(b)))
	if err != nil {
		log.Error("创建「%v」的请求出错: %v", err)
		return false

	}

	if resp, err := client.Do(req); err == nil {

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Error("读取返回数据失败: %v", err)
			return false
		}

		var record BoolResponse
		if err := json.Unmarshal([]byte(body), &record); err != nil {
			log.Error("「%v」刷新能量失败: %v", playerInfo.Name, err)
			return false
		}

		if record.Success == true {
			log.Notice("第%v轮「%v」刷新能量成功, 转换能量%v", _getRound(playerInfo), playerInfo.Name, playerInfo.ConvertedEnergy)
		} else {
			log.Warning("「%v」刷新能量失败, 转换能量%v", playerInfo.Name, playerInfo.ConvertedEnergy)

		}

		resp.Body.Close()
		return true

	} else {
		log.Error("创建请求失败: %v", err)
		return false

	}
}

func _generateEnergy(playerInfo PlayerInfo) PlayerInfo {
	playerInfo.ConvertedEnergy = rand.New(rand.NewSource(time.Now().UnixNano())).Intn(10000) + 50000
	return playerInfo
}

// func _requestNewFriendList(playerInfo PlayerInfo) (*http.Response, error) {
// 	log.Debug("查看「%v」是否有好友申请", playerInfo.Name)

// 	client := &http.Client{}
// 	v := url.Values{}
// 	v.Add("platform", playerInfo.Platform)
// 	v.Add("auth_token", playerInfo.AuthToken)
// 	v.Add("client_version", playerInfo.ClientVersion)

// 	host := fmt.Sprintf("https://universe.walkrgame.com/api/v1/users/friend_invitations?%v", v.Encode())

// 	req, err := _generateRequest(playerInfo, host, "GET", nil)
// 	if req == nil {
// 		return nil, err
// 	}

// 	return client.Do(req)

// }

// func _checkFriendInvitation(playerInfo PlayerInfo) bool {
// 	resp, err := _requestNewFriendList(playerInfo)
// 	if err != nil {
// 		return false
// 	}

// 	body, err := ioutil.ReadAll(resp.Body)
// 	if err != nil {
// 		log.Error("读取返回数据失败: %v", err)
// 		return false
// 	}

// 	var records NewFriendListResponse
// 	if err := json.Unmarshal([]byte(body), &records); err != nil {
// 		log.Error("解析好友列表数据失败: %v", err)
// 		return false
// 	}

// 	if len(records.Data) == 0 {
// 		log.Debug("「%v」没有新的好友申请", playerInfo.Name)
// 		return false
// 	}

// 	for _, friend := range records.Data {
// 		log.Debug("「%v」新的好友申请['%v':%v]", playerInfo.Name, friend.Name, friend.Id)
// 		if _confirmFriend(playerInfo, friend.Id) == true {
// 			log.Debug("「%v」添加好友['%v':%v]成功", playerInfo.Name, friend.Name, friend.Id)
// 		} else {
// 			log.Error("「%v」添加好友['%v':%v]失败", playerInfo.Name, friend.Name, friend.Id)
// 		}
// 	}

// 	return true
// }

// func _confirmFriend(playerInfo PlayerInfo, friendId int) bool {
// 	client := &http.Client{}

// 	confirmFriendRequestJson := ConfirmFriendRequest{
// 		AuthToken:     playerInfo.AuthToken,
// 		ClientVersion: playerInfo.ClientVersion,
// 		Platform:      playerInfo.Platform,
// 		UserId:        friendId,
// 	}
// 	b, err := json.Marshal(confirmFriendRequestJson)
// 	if err != nil {
// 		log.Error("Json Marshal error for %v", err)
// 		return false
// 	}

// 	host := "https://universe.walkrgame.com/api/v1/users/confirm_friend"
// 	req, err := _generateRequest(playerInfo, host, "POST", bytes.NewBuffer([]byte(b)))
// 	if err != nil {
// 		return false
// 	}

// 	if resp, err := client.Do(req); err == nil {
// 		defer resp.Body.Close()

// 		body, err := ioutil.ReadAll(resp.Body)
// 		if err != nil {
// 			log.Error("读取返回数据失败: %v", err)
// 			return false
// 		}

// 		var record BoolResponse
// 		if err := json.Unmarshal([]byte(body), &record); err != nil {
// 			log.Error("通过好友失败: %v", err)
// 			return false
// 		}

// 		return record.Success
// 	} else {
// 		log.Error("请求添加用户失败: %v", err)

// 	}
// 	return false
// }
func _generateRequest(playerInfo PlayerInfo, host string, method string, requestBytes *bytes.Buffer) (*http.Request, error) {
	var req *http.Request
	var err error
	if requestBytes == nil {
		req, err = http.NewRequest(method, host, nil)
	} else {
		req, err = http.NewRequest(method, host, requestBytes)
	}
	if err != nil {
		return nil, errors.New("创建Request失败")
	}

	req.Header.Set("Cookie", playerInfo.Cookie)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Host", "universe.walkrgame.com")
	req.Header.Add("Accept", "*/*")
	req.Header.Add("User-Agent", "Space Walk/2.1.4 (iPhone; iOS 9.1; Scale/2.00)")
	req.Header.Add("Accept-Language", "zh-Hans-CN;q=1, en-CN;q=0.9")

	return req, nil
}

// BI相关
func _getRound(playerInfo PlayerInfo) int {
	currentRound, err := strconv.Atoi(redis.Get(fmt.Sprintf("energy:%v:round", playerInfo.PlayerId())).Val())
	if err != nil || currentRound <= 0 {
		currentRound = 1
	}

	return currentRound
}
func _incrRound(playerInfo PlayerInfo) {
	redis.Incr(fmt.Sprintf("energy:%v:round", playerInfo.PlayerId()))

	time.Sleep(RoundDuration)
}

func (this *PlayerInfo) PlayerId() int {
	playerId, _ := strconv.Atoi(strings.Split(this.AuthToken, ":")[0])
	return playerId
}

func main() {
	ch := make(chan int, 10)

	// 初始化Log
	stdOutput := logging.NewLogBackend(os.Stderr, "", 0)
	stdOutputFormatter := logging.NewBackendFormatter(stdOutput, format)

	logging.SetBackend(stdOutputFormatter)

	redis = goredis.NewClient(redisConf)

	// 读取参数来获得配置文件的名称
	argCount := len(os.Args)
	if argCount == 0 {
		log.Warning("需要输入配置文件名称: 格式 '-c fileName'")
		return
	}

	cmd := flag.String("c", "help", "配置文件名称")
	flag.Parse()
	if *cmd == "help" {
		log.Warning("需要输入配置文件名称: 格式 '-c fileName'")
		return
	}

	if _, err := toml.DecodeFile(*cmd, &config); err != nil {
		log.Error("配置文件有问题: %v", err)
		return
	}
	for _, playerInfo := range config.PlayerInfo {
		go MakeRequest(playerInfo, ch)
	}
	<-ch

}
