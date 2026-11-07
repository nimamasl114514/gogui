package render

// DesignCSS 是 gogui 设计系统样式（美丽核心）。
// 约束：全部颜色/圆角/阴影引用 theme 变量；不写死任何具体色值（transition 除外）；
// 类名一律 gg- 前缀；行为态（hover/active/focus/disabled）都带平滑过渡。
const DesignCSS = `
*,*::before,*::after{box-sizing:border-box}
html,body{height:100%}
body.gg-body{
  margin:0;font-family:var(--font);color:var(--text);
  overflow:hidden;
}
.gg-body,.gg-app,.gg-input,.gg-select,.gg-textarea,.gg-button,.gg-tab,.gg-item{font-family:var(--font)}
.gg-dark{color-scheme:dark}
.gg-light{color-scheme:light}

/* ---- 应用底：双色微光 + 表面层次 ---- */
.gg-app{
  min-height:100vh;display:flex;flex-direction:column;
  background:
    radial-gradient(1100px 540px at 12% -8%, var(--bg-glow-1), transparent 60%),
    radial-gradient(900px 520px at 96% 108%, var(--bg-glow-2), transparent 55%),
    var(--bg);
}
/* 根控件吃掉整个视口高度：后代 Grow() 才有剩余空间可分配 */
.gg-app>*{flex:1 1 auto;min-height:0}

/* ---- 入场动效（级联上浮） ---- */
@keyframes gg-rise{
  from{opacity:0;transform:translateY(12px)}
  to{opacity:1;transform:translateY(0)}
}
.gg-app>*{animation:gg-rise .48s cubic-bezier(.22,1,.36,1) both}
.gg-app>*:nth-child(2){animation-delay:.06s}
.gg-app>*:nth-child(3){animation-delay:.12s}
.gg-app>*:nth-child(4){animation-delay:.18s}
.gg-app>*:nth-child(5){animation-delay:.24s}
.gg-app>*:nth-child(n+6){animation-delay:.3s}
::selection{background:var(--accent);color:#fff}

/* ---- 布局容器 ---- */
.gg-vstack{display:flex;flex-direction:column;min-width:0}
.gg-hstack{display:flex;flex-direction:row;min-width:0}
.gg-spacer{flex:1 1 auto}
.gg-sep{border:none;height:1px;background:var(--border);margin:6px 0}

/* ---- 卡片：玻璃表面 ---- */
.gg-card{
  display:flex;flex-direction:column;
  background:color-mix(in srgb, var(--surface) 88%, transparent);
  border:1px solid var(--border);
  border-radius:var(--radius);
  box-shadow:var(--shadow-md), inset 0 1px 0 rgba(255,255,255,0.05);
  backdrop-filter:blur(14px);
  transition:transform .24s cubic-bezier(.22,1,.36,1), box-shadow .24s ease, border-color .24s ease;
}
.gg-card:hover{
  transform:translateY(-2px);
  box-shadow:var(--shadow-lg), inset 0 1px 0 rgba(255,255,255,0.07);
  border-color:color-mix(in srgb, var(--accent) 22%, var(--border));
}

/* ---- 文本 ---- */
.gg-label{color:var(--text);line-height:1.5;white-space:pre-wrap}
.gg-label.h1{font-size:26px;font-weight:700;letter-spacing:-0.02em;white-space:nowrap}
.gg-label.h2{font-size:19px;font-weight:650;letter-spacing:-0.01em;white-space:nowrap}
.gg-label.h3{font-size:15px;font-weight:600;white-space:nowrap}
.gg-label.metric{font-size:30px;font-weight:700;letter-spacing:-0.02em;line-height:1.2}
.gg-label.nowrap{white-space:nowrap}
.gg-label.muted{color:var(--text-muted)}
.gg-label.small{font-size:12px;color:var(--text-muted)}
.gg-label.strong{font-weight:600}
.gg-label.mono{font-family:"Cascadia Mono",Consolas,"Courier New",monospace}

/* ---- 按钮 ---- */
.gg-button{
  display:inline-flex;align-items:center;justify-content:center;gap:8px;
  padding:10px 18px;border-radius:10px;border:1px solid transparent;
  font-size:14px;font-weight:550;cursor:pointer;user-select:none;
  transition:transform .16s cubic-bezier(.34,1.56,.64,1), box-shadow .18s ease,
             background .18s ease, border-color .18s ease, color .18s ease, filter .18s ease;
}
.gg-button:focus-visible{outline:none;box-shadow:0 0 0 3px var(--focus-ring)}
.gg-button:active{transform:scale(.965)}
.gg-button[disabled]{opacity:.42;pointer-events:none}
.gg-button.gg-secondary{
  background:var(--surface-2);border-color:var(--border);color:var(--text);
}
.gg-button.gg-secondary:hover{
  background:var(--surface-3);border-color:color-mix(in srgb,var(--text-muted) 30%,transparent);
  transform:translateY(-1px);box-shadow:var(--shadow-md);
}
.gg-button.gg-primary{
  background:var(--accent-grad);color:#fff;
  box-shadow:0 4px 18px var(--accent-glow);
}
.gg-button.gg-primary:hover{
  transform:translateY(-1px);filter:saturate(1.15) brightness(1.08);
  box-shadow:0 8px 26px var(--accent-glow);
}
.gg-button.gg-danger{
  background:color-mix(in srgb, var(--danger) 16%, transparent);
  border-color:color-mix(in srgb, var(--danger) 45%, transparent);color:var(--danger);
}
.gg-button.gg-danger:hover{
  background:var(--danger);color:#fff;transform:translateY(-1px);
  box-shadow:0 6px 20px var(--danger-glow);
}
.gg-button.gg-ghost{background:transparent;color:var(--text-muted)}
.gg-button.gg-ghost:hover{background:var(--surface-2);color:var(--text)}

/* ---- 输入 ---- */
.gg-input,.gg-textarea,.gg-select{
  background:var(--surface-2);border:1px solid var(--border);color:var(--text);
  border-radius:10px;padding:10px 12px;font-size:14px;min-width:0;
  transition:border-color .16s ease, box-shadow .16s ease, background .16s ease;
}
.gg-input::placeholder,.gg-textarea::placeholder{color:var(--text-muted);opacity:.8}
.gg-input:focus,.gg-textarea:focus,.gg-select:focus{
  outline:none;border-color:var(--accent);background:var(--surface);
  box-shadow:0 0 0 3px var(--focus-ring);
}
.gg-input[disabled],.gg-textarea[disabled],.gg-select[disabled],.gg-slider[disabled]{opacity:.42;pointer-events:none}
.gg-textarea{resize:vertical;min-height:72px;line-height:1.5}

/* ---- 开关 ---- */
.gg-toggle{
  position:relative;width:44px;height:24px;border-radius:999px;cursor:pointer;
  background:var(--surface-3);border:1px solid var(--border);
  transition:background .2s ease, border-color .2s ease, box-shadow .2s ease;
  flex:none;padding:0;
}
.gg-toggle::after{
  content:"";position:absolute;top:2px;left:2px;width:18px;height:18px;border-radius:50%;
  background:#fff;box-shadow:0 1px 4px rgba(0,0,0,.35);
  transition:transform .22s cubic-bezier(.34,1.56,.64,1);
}
.gg-toggle[aria-checked="true"]{
  background:var(--accent-grad);border-color:transparent;
  box-shadow:0 2px 12px var(--accent-glow);
}
.gg-toggle[aria-checked="true"]::after{transform:translateX(20px)}
.gg-toggle:focus-visible{outline:none;box-shadow:0 0 0 3px var(--focus-ring)}

/* ---- 滑杆 ---- */
.gg-slider{
  -webkit-appearance:none;appearance:none;width:100%;height:6px;border-radius:999px;
  background:linear-gradient(var(--accent-grad), var(--accent-grad)) no-repeat,
             var(--surface-3);
  background-size:var(--fill,0%) 100%;
  cursor:pointer;padding:0;border:none;margin:10px 0;
}
.gg-slider::-webkit-slider-thumb{
  -webkit-appearance:none;appearance:none;width:18px;height:18px;border-radius:50%;
  background:#fff;border:none;box-shadow:0 0 0 0 var(--focus-ring), 0 2px 8px rgba(0,0,0,.45);
  transition:box-shadow .15s ease, transform .15s ease;
}
.gg-slider::-webkit-slider-thumb:hover{transform:scale(1.15);box-shadow:0 0 0 4px var(--focus-ring),0 2px 10px rgba(0,0,0,.5)}
.gg-slider::-webkit-slider-thumb:active{box-shadow:0 0 0 6px var(--focus-ring)}
.gg-slider:focus-visible{outline:none}

/* ---- 进度 ---- */
.gg-progress{
  height:8px;border-radius:999px;background:var(--surface-3);overflow:hidden;
}
.gg-progress-fill{
  height:100%;border-radius:999px;background:var(--accent-grad);
  box-shadow:0 0 12px var(--accent-glow);
  transition:width .55s cubic-bezier(.22,1,.36,1);
}

/* ---- 下拉 ---- */
.gg-selectwrap{position:relative;display:inline-flex;min-width:0}
.gg-selectwrap::after{
  content:"";position:absolute;right:12px;top:50%;width:8px;height:8px;pointer-events:none;
  border-right:2px solid var(--text-muted);border-bottom:2px solid var(--text-muted);
  transform:translateY(-70%) rotate(45deg);
}
.gg-select{appearance:none;-webkit-appearance:none;padding-right:32px;cursor:pointer}
.gg-select option{background:var(--surface);color:var(--text)}

/* ---- 徽标 ---- */
.gg-badge{
  display:inline-flex;align-items:center;padding:3px 10px;border-radius:999px;
  font-size:11px;font-weight:600;letter-spacing:.02em;white-space:nowrap;
  color:var(--accent);background:color-mix(in srgb, var(--accent) 16%, transparent);
}
.gg-badge.ok{color:var(--ok);background:color-mix(in srgb, var(--ok) 15%, transparent)}
.gg-badge.warn{color:var(--warn);background:color-mix(in srgb, var(--warn) 16%, transparent)}
.gg-badge.danger{color:var(--danger);background:color-mix(in srgb, var(--danger) 15%, transparent)}

/* ---- 页签 ---- */
.gg-tabs{display:flex;flex-direction:column;min-width:0}
.gg-tabbar{display:flex;gap:4px;border-bottom:1px solid var(--border);padding:0 4px}
.gg-tab{
  position:relative;padding:10px 16px;font-size:14px;font-weight:550;
  background:none;border:none;color:var(--text-muted);cursor:pointer;
  transition:color .16s ease;
}
.gg-tab:hover{color:var(--text)}
.gg-tab.active{color:var(--accent)}
.gg-tab::after{
  content:"";position:absolute;left:10px;right:10px;bottom:-1px;height:2px;
  border-radius:2px;background:var(--accent-grad);box-shadow:0 0 10px var(--accent-glow);
  transform:scaleX(0);opacity:0;
  transition:transform .26s cubic-bezier(.22,1,.36,1), opacity .2s ease;
}
.gg-tab.active::after{transform:scaleX(1);opacity:1}
.gg-tabpanel{padding-top:14px;display:flex;flex-direction:column}

/* ---- 列表 ---- */
.gg-list{display:flex;flex-direction:column;gap:2px;background:var(--surface-2);
  border:1px solid var(--border);border-radius:12px;padding:6px;min-width:0}
.gg-item{
  display:flex;align-items:center;gap:10px;padding:9px 12px;border-radius:8px;
  background:none;border:none;color:var(--text);font-size:14px;text-align:left;
  cursor:pointer;transition:background .14s ease, color .14s ease;
}
.gg-item:hover{background:color-mix(in srgb, var(--text-muted) 12%, transparent)}
.gg-item.selected{
  background:color-mix(in srgb, var(--accent) 16%, transparent);color:var(--accent);
  font-weight:550;
}
.gg-item[disabled],.gg-tab[disabled]{opacity:.42;pointer-events:none}

/* ---- 图片 ---- */
.gg-image{border-radius:10px;max-width:100%;object-fit:cover}

/* ---- 滚动条 ---- */
::-webkit-scrollbar{width:10px;height:10px}
::-webkit-scrollbar-thumb{background:var(--surface-3);border-radius:999px;border:2px solid transparent;background-clip:content-box}
::-webkit-scrollbar-thumb:hover{background:color-mix(in srgb, var(--text-muted) 40%, transparent);border:2px solid transparent;background-clip:content-box}
::-webkit-scrollbar-track{background:transparent}

/* ---- 动效可关（golden 截图时注入 .gg-static 冻结一切动效） ---- */
.gg-static *,.gg-static *::before,.gg-static *::after{
  transition:none !important;animation:none !important;caret-color:transparent !important;
}
@media (prefers-reduced-motion: reduce){
  /* 尊重系统「减少动画」：仅作用于未强制动画的应用（html.gg-force-anim 除外）。 */
  html:not(.gg-force-anim) .gg-app *,
  html:not(.gg-force-anim) .gg-app *::before,
  html:not(.gg-force-anim) .gg-app *::after{
    transition-duration:.01ms !important;animation-duration:.01ms !important;
  }
}
`
