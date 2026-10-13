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
