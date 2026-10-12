package api

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
}

// userResponse 用在 /api/me，注册成功也返回它
type userResponse struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}
