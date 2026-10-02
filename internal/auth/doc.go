// Package auth 负责身份凭证的签发与校验。
//
// 职责：
//   - 用 bcrypt 哈希与校验用户密码，明文密码绝不落库
//   - 签发与校验 JWT，作为已登录用户的会话凭证
//
// 本包只处理「凭证是否可信」，不知道用户表长什么样，也不碰数据库。
// 用户的查找属于 internal/user，持久化属于 internal/store。
package auth
