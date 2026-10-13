package room

// message.go —— 房间事件的 data 结构体（协议 §6.2）。
// 名字与字段名一律以协议文档为准，这里只是它的实现。

// seatState 是一个座位的对外状态。
type seatState struct {
	Seat      int    `json:"seat"`
	UserID    int64  `json:"userId"`
	Username  string `json:"username"`
	Ready     bool   `json:"ready"`
	Connected bool   `json:"connected"`
	IsOwner   bool   `json:"isOwner"`
}

// roomCreated 发给房主，告诉他座位与房间号。
type roomCreated struct {
	RoomID string `json:"roomId"`
	Seat   int    `json:"seat"`
}

// roomJoined 只发给加入者，带他自己的座位。
type roomJoined struct {
	RoomID string       `json:"roomId"`
	MySeat int          `json:"mySeat"`
	Seats  []*seatState `json:"seats"`
}

// roomSeats 广播给全房间，座位有变化时发。
type roomSeats struct {
	Seats []*seatState `json:"seats"`
}

type roomReadyChanged struct {
	Seat  int  `json:"seat"`
	Ready bool `json:"ready"`
}

type roomLeft struct {
	Seat int `json:"seat"`
}

type roomSeatVacated struct {
	Seat   int    `json:"seat"`
	Reason string `json:"reason"`
}

// roomClosed 广播给房间里还剩下的人：这个房间没了，回大厅去。
//
// 必须有一条明确的收尾事件。不发的话，剩下的人只会发现"发什么指令都不对劲"，
// 而不知道房间已经不存在了 —— 那比房间解散本身更糟。
type roomClosed struct {
	RoomID string `json:"roomId"`
	Reason string `json:"reason"`
}

// 关房理由（协议 §6.2）。两条都指向同一件事：四个人凑不齐了，这局散了。
// 放在这里而不是各自产生它的文件里 —— 客户端按 reason 分支，它需要一份完整的表。
const (
	// reasonPlayerTimeout 有人掉线且没回来（协议 §4.5 的 120 秒保座窗口）。
	reasonPlayerTimeout = "playerTimeout"
	// reasonPlayerLeft 对局中有人离开（协议 §5.2：对局中视为认输/退出）。
	reasonPlayerLeft = "playerLeft"
)
