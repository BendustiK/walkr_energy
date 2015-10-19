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
	"net/url"
	"os"
	"sort"
	"time"

	"github.com/BurntSushi/toml"
	goerrors "github.com/go-errors/errors"

	"github.com/op/go-logging"
)

var RoundDuration = 2 * time.Minute
var WaitDuration = 5 * time.Minute
var MAX_JOIN_TIMES = 5
var FleetInvitationCount = make(map[int]int)

const (
	COMMENT_JOINED = "我进来啦，我会在五分钟之后自动退队。如果退队的时候还没有捐献完毕，不要着急，重新邀请就好。不过请记住，同一舰队邀请数量达到五次，我会忽略邀请的。谢谢!"
	COMMENT_LEAVE  = "关于离开舰队, 大家有话说."
)

var LeaveComments = []string{
	"「晒宁」说: 0110 0001 0110 0000 1011 0001 1111 1000",
	"「卷儿」说: 萌萌的我要走啦, 不要想念我, 开启你星辰大海的征途吧ε==(づ′▽`)づ",
	"「天选」说: 距离飞船爆炸还有30秒, 我就先走啦",
	"「抹香」说: 虽然我很想跟你一起飞下去, 但是卷卷老婆叫我回家吃饭了, 所以拜拜啦(*￣3￣)╭",
	"「露露」说: 本宝宝要去吃饭啦(^○^)不要想念我, 你们加油哦（＾∇＾）",
	"「露露」还说: 哈喽, 豆浆帮飞开始了～duang！",
	"「那啥」说: 本公举不是轻易帮飞的, 帮你飞了记得回报我洗白白去吧, 我先去床上等你了(￣^￣)ゞ",
	"「那啥」还说: 一路顺风欢迎下次使用; 豆浆牌帮飞, 就是香; 天选那个棒; 容我思考一下.",
	"「那啥」正经说: あなたのことが好きだけどもうあきらめます じゃさよなら",
	"「肥兔纸」说: _ . . .  _ . _ _  .",
	"「大空」说: Have a good time～୧(๑•̀⌄•́๑)૭",
	"「桃乐丝」说: 啊朋友再见, 啊朋友再见吧再见吧再见吧~",
	"「大河」说: 海阔天空任你浪, 良辰不奉陪了ε=ε=ε=ε=ε=ε=┌(;￣◇￣)┘",
	"「七大喵」说: 虽然我貌美如花, 人见人爱, 花见花开. 可是我就如春风一样无法被抓住…so~白白了, 放手!再见!",
	"「会长」说: 任务已完成, AI5102号关闭.",
	"「花仙子小太阳」说: 美丽的警察姐姐温馨提示: 右侧通行 限速30 (*´ｪ`*)",
	"「Saber」说: 祝您旅途愉快，单飞顺利括弧笑:)",
	"「树蛙」说: 耗子和树蛙说 哎咦！我们都不会飞！吱～呱～ᶘ ᵒᴥᵒᶅ",
	"「白树」说: Namárië! Nai hiruvalyë Valimar. Nai elyë hiruva. 愿伊鲁维塔与你同在(´-灬-‘)",
}

type CommentRequest struct {
	AuthToken     string `json:"auth_token"`
	ClientVersion string `json:"client_version"`
	Platform      string `json:"platform"`
	Locale        string `json:"locale"`
	Text          string `json:"text"`
}

type ConfirmFriendRequest struct {
	AuthToken     string `json:"auth_token"`
	UserId        int    `json:"user_id"`
	ClientVersion string `json:"client_version"`
	Platform      string `json:"platform"`
}

// 1. 传说列表Resp
type EpicListResponse struct {
	Epics []Epic `json:"epics"`
}
type Epic struct {
	Name             string `json:"name"`
	InvitationCounts int    `json:"invitation_counts"`
}

// 2. 飞传说中的舰队列表Resp
type FleetListResponse struct {
	Fleets []Fleet `json:"fleets"`
}
type Fleet struct {
	Id        int     `json:"id"`
	Name      string  `json:"name"`
	IsInvited bool    `json:"is_invited"`
	Captain   Captain `json:"captain"`
	Quality   int
}
type Captain struct {
	Name string `json:"name"`
}

// 3. 好友申请
type NewFriendListResponse struct {
	Data []Friend `json:"data"`
}
type Friend struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

type BoolResponse struct {
	Success bool
}

type PlayerInfo struct {
	Name            string `json:"-"`
	AuthToken       string `json:"auth_token"`
	ClientVersion   string `json:"client_version"`
	Platform        string `json:"platform"`
	Locale          string `json:"locale"`
	Cookie          string `json:"-"`
	IfNoneMatch     string `json:"-"`
	ConvertedEnergy int    `json:"-"`
}

type PlayerInfos struct {
	PlayerInfo []PlayerInfo
}

var round = 1
var config PlayerInfos
var log = logging.MustGetLogger("Walkr")
var format = logging.MustStringFormatter(
	"%{color}%{time:15:04:05.000} %{shortfile} ▶ %{level:.4s} %{id:03x}%{color:reset} %{message}",
)

func GetJoinedTimes(fleetId int) int {
	if times, isOk := FleetInvitationCount[fleetId]; isOk == true {
		return times
	}

	return 0
}

func MakeRequest() {
	defer func() {
		if r := recover(); r != nil {
			msg := goerrors.Wrap(r, 2).ErrorStack()
			log.Error("程序挂了: %v", msg)
		}
	}()

	// 1. 获取传说列表
	// 2. 获取舰队列表
	// 3. 加入邀请的舰队
	// 4. 留言说明几分钟退出
	// 5. 退出舰队
	for _, playerInfo := range config.PlayerInfo {
		log.Warning("=====================「%v」的第%v次循环 =====================", playerInfo.Name, round)

		// 获取传说列表
		var resp *http.Response
		var err error

		// 每十轮判断是否有好友申请
		if round%10 == 0 {
			_checkFriendInvitation(playerInfo)

		}

		// 获取传说列表
		resp, err = _requestEpicList(playerInfo)
		if err != nil {
			log.Error("获取传说列表失败: %v", err)
			continue
		}
		if resp.Body != nil {
			defer resp.Body.Close()

		}

		hasInvitation := _checkInvitationCount(resp)
		if hasInvitation == false {
			log.Notice("当前没有邀请的传说, 等待下一次刷新")
			continue
		}

		// 如果有传说, 随便获取一个传说列表, 找到邀请的传说
		resp, err = _requestFleetList(playerInfo)
		if err != nil {
			log.Error("获取舰队列表失败: %v", err)
			continue
		}
		if resp.Body != nil {
			defer resp.Body.Close()

		}

		fleetId := _getInvitationFleetId(resp)
		if fleetId <= 0 {
			log.Notice("当前没有邀请的舰队, 等待下次刷新")
			continue
		}

		appliedOk := _applyInvitedFleet(playerInfo, fleetId)
		if appliedOk == false {
			log.Notice("加入舰队(%v)失败, 等待下次刷新", fleetId)
			continue
		}

		// 更新加入同一舰队的数量
		FleetInvitationCount[fleetId] = GetJoinedTimes(fleetId) + 1

		_leaveComment(playerInfo, fleetId, COMMENT_JOINED)

		// 5分钟之后自动退出
		time.Sleep(WaitDuration)

		_leaveComment(playerInfo, fleetId, COMMENT_LEAVE)

		leaveComment := LeaveComments[rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(LeaveComments))]
		_leaveComment(playerInfo, fleetId, leaveComment)

		leaveCount := 0
		for leaveCount < 3 {
			if leaveOk := _leaveFleet(playerInfo, fleetId); leaveOk == true {
				break
			} else {
				leaveCount += 1
			}
		}

	}

	round += 1
}

func _requestNewFriendList(playerInfo PlayerInfo) (*http.Response, error) {
	log.Debug("查看是否有好友申请")

	client := &http.Client{}
	v := url.Values{}
	v.Add("platform", playerInfo.Platform)
	v.Add("auth_token", playerInfo.AuthToken)
	v.Add("client_version", playerInfo.ClientVersion)

	host := fmt.Sprintf("https://universe.walkrgame.com/api/v1/users/friend_invitations?%v", v.Encode())

	req, err := _generateRequest(playerInfo, host, "GET", nil)
	if req == nil {
		return nil, err
	}

	return client.Do(req)

}

func _checkFriendInvitation(playerInfo PlayerInfo) bool {
	resp, err := _requestNewFriendList(playerInfo)
	if err != nil {
		return false
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error("读取返回数据失败: %v", err)
		return false
	}

	var records NewFriendListResponse
	if err := json.Unmarshal([]byte(body), &records); err != nil {
		log.Error("解析好友列表数据失败: %v", err)
		return false
	}

	if len(records.Data) == 0 {
		log.Debug("没有新的好友申请")
		return false
	}

	for _, friend := range records.Data {
		log.Debug("新的好友申请['%v':%v]", friend.Name, friend.Id)
		if _confirmFriend(playerInfo, friend.Id) == true {
			log.Debug("添加好友['%v':%v]成功", friend.Name, friend.Id)
		} else {
			log.Error("添加好友['%v':%v]失败", friend.Name, friend.Id)
		}
	}

	return true
}

func _confirmFriend(playerInfo PlayerInfo, friendId int) bool {
	client := &http.Client{}

	confirmFriendRequestJson := ConfirmFriendRequest{
		AuthToken:     playerInfo.AuthToken,
		ClientVersion: playerInfo.ClientVersion,
		Platform:      playerInfo.Platform,
		UserId:        friendId,
	}
	b, err := json.Marshal(confirmFriendRequestJson)
	if err != nil {
		log.Error("Json Marshal error for %v", err)
		return false
	}

	host := "https://universe.walkrgame.com/api/v1/users/confirm_friend"
	req, err := _generateRequest(playerInfo, host, "POST", bytes.NewBuffer([]byte(b)))
	if err != nil {
		return false
	}

	if resp, err := client.Do(req); err == nil {
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Error("读取返回数据失败: %v", err)
			return false
		}

		var record BoolResponse
		if err := json.Unmarshal([]byte(body), &record); err != nil {
			log.Error("通过好友失败: %v", err)
			return false
		}

		return record.Success
	}
	return false
}

func _requestEpicList(playerInfo PlayerInfo) (*http.Response, error) {
	client := &http.Client{}
	v := url.Values{}
	v.Add("locale", playerInfo.Locale)
	v.Add("platform", playerInfo.Platform)
	v.Add("auth_token", playerInfo.AuthToken)
	v.Add("client_version", playerInfo.ClientVersion)

	host := fmt.Sprintf("https://universe.walkrgame.com/api/v1/epics?%v", v.Encode())
	req, err := _generateRequest(playerInfo, host, "GET", nil)
	if err != nil {
		return nil, err
	}

	return client.Do(req)

}

func _requestFleetList(playerInfo PlayerInfo) (*http.Response, error) {
	client := &http.Client{}
	v := url.Values{}
	v.Add("locale", playerInfo.Locale)
	v.Add("platform", playerInfo.Platform)
	v.Add("auth_token", playerInfo.AuthToken)
	v.Add("client_version", playerInfo.ClientVersion)
	v.Add("country_code", "US")
	v.Add("epic_id", "14")
	v.Add("limit", "30")
	v.Add("name", "")
	v.Add("offset", "0")

	host := fmt.Sprintf("https://universe.walkrgame.com/api/v1/fleets?%v", v.Encode())
	req, err := _generateRequest(playerInfo, host, "GET", nil)
	if err != nil {
		return nil, err
	}

	return client.Do(req)
}

func _applyInvitedFleet(playerInfo PlayerInfo, fleetId int) bool {
	client := &http.Client{}
	b, err := json.Marshal(playerInfo)
	if err != nil {
		log.Error("Json Marshal error for %v", err)
		return false
	}

	host := fmt.Sprintf("https://universe.walkrgame.com/api/v1/fleets/%v/apply", fleetId)
	req, err := _generateRequest(playerInfo, host, "POST", bytes.NewBuffer([]byte(b)))
	if err != nil {
		return false
	}

	if resp, err := client.Do(req); err == nil {
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Error("读取返回数据失败: %v", err)
			return false
		}

		var record BoolResponse
		if err := json.Unmarshal([]byte(body), &record); err != nil {
			log.Error("加入舰队失败: %v", err)
			return false
		}

		log.Notice("已经加入舰队[%v], 等待起飞", fleetId)

		return record.Success
	}

	return false
}

func _leaveComment(playerInfo PlayerInfo, fleetId int, comment string) bool {
	client := &http.Client{}

	commentRequestJson := CommentRequest{
		AuthToken:     playerInfo.AuthToken,
		ClientVersion: playerInfo.ClientVersion,
		Platform:      playerInfo.Platform,
		Locale:        playerInfo.Locale,
		Text:          comment,
	}
	b, err := json.Marshal(commentRequestJson)
	if err != nil {
		log.Error("Json Marshal error for %v", err)
		return false
	}

	host := fmt.Sprintf("https://universe.walkrgame.com/api/v1/fleets/%v/comment", fleetId)
	req, err := _generateRequest(playerInfo, host, "POST", bytes.NewBuffer([]byte(b)))
	if err != nil {
		return false
	}

	if resp, err := client.Do(req); err == nil {
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Error("读取返回数据失败: %v", err)
			return false
		}

		var record BoolResponse
		if err := json.Unmarshal([]byte(body), &record); err != nil {
			log.Error("留言失败: %v", err)
			return false
		}

		log.Notice("已经留言(%v)", comment)
		return record.Success
	}

	return false
}

func _leaveFleet(playerInfo PlayerInfo, fleetId int) bool {
	client := &http.Client{}

	b, err := json.Marshal(playerInfo)
	if err != nil {
		log.Error("Json Marshal error for %v", err)
		return false
	}

	host := fmt.Sprintf("https://universe.walkrgame.com/api/v1/fleets/%v/leave", fleetId)
	req, err := _generateRequest(playerInfo, host, "POST", bytes.NewBuffer([]byte(b)))
	if err != nil {
		return false
	}

	if resp, err := client.Do(req); err == nil {
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Error("读取返回数据失败: %v", err)
			return false
		}

		var record BoolResponse
		if err := json.Unmarshal([]byte(body), &record); err != nil {
			log.Error("离开舰队失败: %v", err)
			return false
		}

		log.Notice("退出舰队[%v]成功", fleetId)
		return record.Success
	}

	return false
}

func _checkInvitationCount(resp *http.Response) bool {
	isInvitation := false

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error("读取返回数据失败: %v", err)
		return isInvitation
	}

	var records EpicListResponse
	if err := json.Unmarshal([]byte(body), &records); err != nil {
		log.Error("解析传说列表数据失败: %v", err)
		return isInvitation
	}

	for _, epic := range records.Epics {
		log.Debug("传说[%v], 邀请数量[%v]", epic.Name, epic.InvitationCounts)
		if epic.InvitationCounts > 0 {
			isInvitation = true
		}
	}

	return isInvitation
}

func _getInvitationFleetId(resp *http.Response) int {
	fleetId := 0

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error("读取返回数据失败: %v", err)
		return fleetId
	}

	var records FleetListResponse
	if err := json.Unmarshal([]byte(body), &records); err != nil {
		log.Error("解析传说列表数据失败: %v", err)
		return fleetId
	}

	var fleets Fleets
	for _, fleet := range records.Fleets {
		if fleet.IsInvited == true {
			fleet.Quality = GetJoinedTimes(fleet.Id)

			if fleet.Quality < MAX_JOIN_TIMES {
				fleets = append(fleets, fleet)
			} else {
				log.Error("舰队[%v:%v] by (%v): 已经到达自动帮飞次数上限, 加入黑名单", fleet.Name, fleet.Id, fleet.Captain.Name)
			}

		}
	}

	if len(fleets) > 0 {
		// 加入次数少的队伍优先进入, 防止恶意邀请阻塞进程
		sort.Sort(fleets)

		firstFleet := fleets[0]
		log.Notice("舰队[%v:%v] by (%v): 正在邀请, 优先度(%v)", firstFleet.Name, firstFleet.Id, firstFleet.Captain.Name, firstFleet.Quality)
		return firstFleet.Id
	}

	return 0
}

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
	if playerInfo.IfNoneMatch != "" {
		req.Header.Add("If-None-Match", playerInfo.IfNoneMatch)
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Host", "universe.walkrgame.com")
	req.Header.Add("Accept", "*/*")
	req.Header.Add("User-Agent", "Space Walk/2.1.2 (iPhone; iOS 9.0.2; Scale/2.00)")
	req.Header.Add("Accept-Language", "zh-Hans-CN;q=1")

	return req, nil
}

func main() {
	// 初始化Log
	stdOutput := logging.NewLogBackend(os.Stderr, "", 0)
	stdOutputFormatter := logging.NewBackendFormatter(stdOutput, format)

	logging.SetBackend(stdOutputFormatter)

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

	for true {
		MakeRequest()
		time.Sleep(RoundDuration)
	}

}

type Fleets []Fleet

func (ms Fleets) Len() int {
	return len(ms)
}

func (ms Fleets) Less(i, j int) bool {
	return ms[i].Quality < ms[j].Quality
}

func (ms Fleets) Swap(i, j int) {
	ms[i], ms[j] = ms[j], ms[i]
}
