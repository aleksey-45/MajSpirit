// Package api 是 HTTP 接口层，对应任务书 §2 与 §6 的请求入口。
//
// 职责：
//   - 路由与中间件（日志、恢复、鉴权）
//   - 请求解析、参数校验、响应编码
//   - 把 HTTP 语义翻译成 internal/user 与 internal/store 的调用
//
// 本包不含业务规则：校验通过后即向下调用用例层，错误码与错误信息
// 在此处统一映射。所有 handler 都应当是薄薄的一层。
package api
