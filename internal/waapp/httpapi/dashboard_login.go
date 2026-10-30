package httpapi

import (
	"html/template"
	"net/http"
)

const dashboardLoginMaxFormBytes = 4096

type dashboardLoginPageData struct {
	Next  string
	Error string
}

var dashboardLoginTemplate = template.Must(template.New("dashboard-login").Parse(dashboardLoginHTML))

func handleDashboardLogin(w http.ResponseWriter, r *http.Request, auth dashboardAuthConfig) {
	next := dashboardSafeRedirectTarget(r.URL.Query().Get("next"))
	switch r.Method {
	case http.MethodGet:
		if authenticatedDashboardRequest(r, auth) {
			http.Redirect(w, r, next, http.StatusSeeOther)
			return
		}
		renderDashboardLogin(w, http.StatusOK, next, "")
	case http.MethodPost:
		handleDashboardLoginPost(w, r, auth, next)
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func handleDashboardLoginPost(w http.ResponseWriter, r *http.Request, auth dashboardAuthConfig, next string) {
	r.Body = http.MaxBytesReader(w, r.Body, dashboardLoginMaxFormBytes)
	if err := r.ParseForm(); err != nil {
		renderDashboardLogin(w, http.StatusBadRequest, next, "登录请求无效")
		return
	}
	next = dashboardSafeRedirectTarget(r.FormValue("next"))
	if !validDashboardPassword(r.FormValue("password"), auth) {
		renderDashboardLogin(w, http.StatusUnauthorized, next, "密码不正确")
		return
	}
	cookie, err := newDashboardSessionCookie(r, auth)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create dashboard session failed"})
		return
	}
	http.SetCookie(w, cookie)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func handleDashboardLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	http.SetCookie(w, expiredDashboardSessionCookie(r))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func renderDashboardLogin(w http.ResponseWriter, status int, next string, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = dashboardLoginTemplate.Execute(w, dashboardLoginPageData{Next: next, Error: message})
}

const dashboardLoginHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>WA 登录</title>
<style>
:root{color-scheme:light dark;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#07130d;color:#e9fff0}body{min-height:100dvh;margin:0;display:grid;place-items:center;background:radial-gradient(circle at top,#14532d 0,#07130d 48%,#020617 100%)}main{width:min(92vw,380px);border:1px solid rgba(255,255,255,.12);border-radius:24px;background:rgba(2,6,23,.76);box-shadow:0 24px 70px rgba(0,0,0,.36);padding:32px;backdrop-filter:blur(14px)}.brand{display:flex;align-items:center;gap:12px;margin-bottom:24px}.mark{display:grid;place-items:center;width:44px;height:44px;border-radius:14px;background:#22c55e;color:#052e16;font-weight:800}.title{margin:0;font-size:22px}.sub{margin:4px 0 0;color:#a7f3d0;font-size:13px}label{display:grid;gap:8px;margin-top:16px;color:#bbf7d0;font-size:13px}input{box-sizing:border-box;width:100%;border:1px solid rgba(187,247,208,.22);border-radius:12px;background:rgba(15,23,42,.92);color:#f8fafc;padding:12px 14px;font:inherit;outline:none}input:focus{border-color:#22c55e;box-shadow:0 0 0 3px rgba(34,197,94,.18)}button{width:100%;margin-top:22px;border:0;border-radius:12px;background:#22c55e;color:#052e16;padding:12px 14px;font:inherit;font-weight:700;cursor:pointer}button:hover{background:#4ade80}.hint{margin:14px 0 0;color:#86efac;font-size:12px;line-height:1.6}.error{margin:0 0 14px;border:1px solid rgba(248,113,113,.28);border-radius:12px;background:rgba(127,29,29,.32);color:#fecaca;padding:10px 12px;font-size:13px}
</style>
</head>
<body>
<main>
  <div class="brand"><div class="mark">WA</div><div><h1 class="title">登录 WA 管理</h1><p class="sub">输入访问密码，登录状态保留 7 天</p></div></div>
  {{if .Error}}<p class="error">{{.Error}}</p>{{end}}
  <form method="post" action="/login" autocomplete="on">
    <input type="hidden" name="next" value="{{.Next}}">
    <label>访问密码<input name="password" type="password" autocomplete="current-password" required autofocus></label>
    <button type="submit">登录</button>
    <p class="hint">关闭浏览器后仍会保持登录；如需退出可访问 <code>/logout</code>。</p>
  </form>
</main>
</body>
</html>`
