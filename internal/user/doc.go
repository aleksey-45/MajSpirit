// Package user 实现用户系统的用例层。
//
// 职责（对应任务书 §2）：
//   - 注册：校验用户名与密码强度，委托 auth 哈希密码，写入 store
//   - 登入：从 store 取出用户，委托 auth 校验密码，签发会话凭证
//   - 登出：使当前会话凭证失效
//
// 本包依赖 auth 与 store 的接口，不直接依赖具体的数据库实现，
// 也不处理 HTTP 细节 —— 请求解析与响应编码属于 internal/api。
package user
