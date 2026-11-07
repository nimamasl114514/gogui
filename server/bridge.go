package server

import "github.com/nimamasl114514/gogui/render"

// BridgeJS 是注入浏览器的事件桥 + keyed DOM morph 引擎。
//
// 三件事：
//  1. 事件委托捕获：click/change/input 冒泡到 document，找到最近的
//     [data-gid] 且带 data-ev 的控件，POST 到 /gg/ev；
//  2. 补丁应用：把响应/SSE 里的新 HTML 与现有 DOM 做 keyed morph
//     （key = data-gid|data-gv），只改差异：焦点、光标位置、CSS 过渡全保留；
//  3. SSE 订阅：Go 侧 Update（定时器等）触发的重渲染实时下发。
const BridgeJS = `(function () {
  'use strict';

  // ---------- 补丁应用（keyed morph） ----------
  function keyOf(el) {
    return (el.getAttribute && el.getAttribute('data-gid')) + '|' +
           (el.getAttribute && el.getAttribute('data-gv') || '');
  }

  function morphEl(t, s) {
    if (t.tagName !== s.tagName || keyOf(t) !== keyOf(s)) {
      t.replaceWith(s);
      return;
    }
    // 属性同步
    for (var i = t.attributes.length - 1; i >= 0; i--) {
      var a = t.attributes[i].name;
      if (!s.hasAttribute(a)) t.removeAttribute(a);
    }
    for (var j = 0; j < s.attributes.length; j++) {
      var b = s.attributes[j];
      if (t.getAttribute(b.name) !== b.value) t.setAttribute(b.name, b.value);
    }
    // 表单控件属性同步（value 是 property 不是 attribute）
    var tn = t.tagName;
    if (tn === 'TEXTAREA') {
      var wantText = s.textContent;
      if (t.value !== wantText && document.activeElement !== t) t.value = wantText;
    } else if (tn === 'SELECT') {
      // 同步选项选中态
      var so = s.options, to = t.options;
      if (to.length === so.length) {
        for (var k = 0; k < so.length; k++) {
          if (to[k].selected !== so[k].selected) to[k].selected = so[k].selected;
        }
      }
    } else if (tn === 'INPUT') {
      var wantVal = s.value;
      if (wantVal !== undefined && t.value !== wantVal && document.activeElement !== t) {
        t.value = wantVal;
      }
      // range 的 value 属性变更时 property 也要跟上（且不干扰正在拖动）
      if (t.type === 'range' && t.getAttribute('value') !== s.getAttribute('value')) {
        t.value = s.getAttribute('value');
      }
    }
    // 子节点：叶子（无元素子节点）同步文本；容器递归 keyed morph。
    var sHasEl = s.children.length > 0;
    if (!sHasEl) {
      var cur = t.childNodes, got = '';
      for (var m = 0; m < cur.length; m++) got += cur[m].nodeValue || '';
      var want = s.textContent;
      if (got !== want) t.textContent = want;
      return;
    }
    morphChildren(t, s);
  }

  function morphChildren(t, s) {
    var sKids = Array.prototype.slice.call(s.children);
    // 删除多余
    while (t.children.length > sKids.length) t.removeChild(t.lastElementChild);
    for (var i = 0; i < sKids.length; i++) {
      var cur = t.children[i];
      var src = sKids[i];
      if (!cur) { t.appendChild(src.cloneNode(true)); continue; }
      if (keyOf(cur) === keyOf(src)) { morphEl(cur, src); continue; }
      // key 不匹配：找后面的同 key 节点搬过来，否则克隆插入
      var match = null;
      for (var j = i + 1; j < t.children.length; j++) {
        if (keyOf(t.children[j]) === keyOf(src)) { match = t.children[j]; break; }
      }
      if (match) {
        t.insertBefore(match, cur);
        morphEl(match, src);
      } else {
        t.insertBefore(src.cloneNode(true), cur);
      }
    }
  }

  function applyPatch(html) {
    window.__ggFrames = (window.__ggFrames || 0) + 1;
    var tpl = document.createElement('template');
    tpl.innerHTML = html;
    var src = tpl.content.firstElementChild;
    var root = document.querySelector('.gg-app');
    if (!root || !src) return;
    morphEl(root, src);
  }

  // ---------- 事件委托捕获 ----------
  function post(gid, type, value) {
    window.__ggPostAttempts = (window.__ggPostAttempts || 0) + 1;
    var body = JSON.stringify({ widget_id: gid, type: type, value: value || '' });
    // 网络级失败自动重试一次（本机/CI 的 loopback 偶发瞬断不应丢用户事件）。
    function attempt(n) {
      var t0 = performance.now();
      fetch('/gg/ev', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: body
      }).then(function (r) { return r.json(); })
        .then(function (resp) {
          window.__ggPostOk = (window.__ggPostOk || 0) + 1;
          var t1 = performance.now();
          applyPatch(resp.patch);
          var t2 = performance.now();
          // 前端耗时回填：事件→响应(rtt) 与 morph 应用耗时，服务端 join 进 trace span。
          if (resp.trace_id) {
            fetch('/gg/perf', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({
                trace_id: resp.trace_id,
                rtt_us: Math.round((t1 - t0) * 1000),
                morph_us: Math.round((t2 - t1) * 1000)
              })
            });
          }
        })
        .catch(function (e) {
          if (n < 2) {
            window.__ggPostRetries = (window.__ggPostRetries || 0) + 1;
            setTimeout(function () { attempt(n + 1); }, 150);
            return;
          }
          window.__ggLastPostErr = String(e);
          console.warn('gogui bridge post failed:', e);
          postLogWarn('bridge post failed: ' + e, '/gg/ev');
        });
    }
    attempt(0);
  }

  function ggIdOf(el) {
    return (el.getAttribute('data-gg-id') || el.getAttribute('data-gid')) || '';
  }

  function target(ev) {
    var el = ev.target;
    while (el && el !== document.body) {
      if (el.getAttribute && ggIdOf(el) && el.getAttribute('data-ev')) return el;
      el = el.parentElement;
    }
    return null;
  }

  document.addEventListener('click', function (ev) {
    var el = target(ev);
    if (!el) return;
    if (el.tagName === 'BUTTON' || el.getAttribute('role') === 'switch' ||
        el.classList.contains('gg-tab') || el.classList.contains('gg-item')) {
      ev.preventDefault();
      var gid = ggIdOf(el);
      var isToggle = el.getAttribute('role') === 'switch';
      var val = el.getAttribute('data-gv') || '';
      if (isToggle) {
        // 开关：翻转语义在 Go 侧处理，发 change 携带新状态。
        var on = el.getAttribute('aria-checked') !== 'true';
        post(gid, 'change', on ? 'true' : 'false');
      } else {
        post(gid, 'click', val);
      }
    }
  });

  // 输入类：input 事件实时同步（每控件独立 timer，跨控件输入不互相吞）
  var inputTimers = {};
  function deferInput(el) {
    var gid = ggIdOf(el);
    var val = el.value;
    if (inputTimers[gid]) clearTimeout(inputTimers[gid]);
    inputTimers[gid] = setTimeout(function () {
      delete inputTimers[gid];
      post(gid, 'input', val);
    }, 40);
  }

  document.addEventListener('input', function (ev) {
    var el = ev.target;
    if (el.matches && el.matches('.gg-slider')) {
      post(ggIdOf(el), 'input', el.value);
      return;
    }
    if (el.matches && el.matches('.gg-input,.gg-textarea') && ggIdOf(el)) {
      deferInput(el);
    }
  });

  document.addEventListener('change', function (ev) {
    var el = ev.target;
    if (el.matches && el.matches('.gg-select') && ggIdOf(el)) {
      post(el.getAttribute('data-gid'), 'change', el.value);
    }
  });

  // 键盘可达性：Enter 触发按钮点击（浏览器原生 button 已处理，这里兜底 switch）
  document.addEventListener('keydown', function (ev) {
    if (ev.key !== 'Enter') return;
    var el = target(ev);
    if (el && el.getAttribute('role') === 'switch') {
      el.click();
    }
  });

  // ---------- 前端错误回流（不开 DevTools 也能知道崩没崩） ----------
  function postLog(level, msg, where, stack) {
    try {
      fetch('/gg/log', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          level: level, msg: String(msg == null ? '' : msg),
          where: String(where == null ? '' : where),
          stack: String(stack == null ? '' : stack)
        })
      });
    } catch (e) { /* 日志本身失败不再递归 */ }
  }

  window.addEventListener('error', function (e) {
    postLog('error', e.message || 'error',
      (e.filename || '') + ':' + (e.lineno || 0) + ':' + (e.colno || 0),
      (e.error && e.error.stack) || '');
  });
  window.addEventListener('unhandledrejection', function (e) {
    var r = e.reason;
    postLog('error', 'unhandledrejection: ' + ((r && r.message) || r), '',
      (r && r.stack) || '');
  });

  function postLogWarn(msg, where) {
    try {
      fetch('/gg/log', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ level: 'warn', msg: msg, where: where, stack: '' })
      });
    } catch (e) {}
  }

  // ---------- 布局内省（按需采集控件真实盒模型 + 计算样式） ----------
  function collectLayout() {
    var items = [];
    var all = document.querySelectorAll('[data-gid]');
    for (var i = 0; i < all.length; i++) {
      var el = all[i];
      var r = el.getBoundingClientRect();
      var cs = getComputedStyle(el);
      items.push({
        gid: el.getAttribute('data-gid'),
        tag: el.tagName.toLowerCase(),
        cls: el.className,
        x: Math.round(r.x * 10) / 10, y: Math.round(r.y * 10) / 10,
        w: Math.round(r.width * 10) / 10, h: Math.round(r.height * 10) / 10,
        display: cs.display, visibility: cs.visibility, opacity: cs.opacity,
        fontSize: cs.fontSize, fontWeight: cs.fontWeight,
        color: cs.color, background: cs.backgroundColor,
        borderRadius: cs.borderRadius,
        animation: cs.animationName + "|" + cs.animationDuration + "|" + cs.animationPlayState
      });
    }
    return {
      viewport: { w: window.innerWidth, h: window.innerHeight },
      items: items
    };
  }

  function postLayout(nonce) {
    var data = collectLayout();
    data.nonce = nonce;
    try {
      fetch('/gg/layout-report', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data)
      });
    } catch (e) { postLogWarn('layout report failed: ' + e, '/gg/layout-report'); }
  }

  // 桥就绪标记（测试等待用）：监听器/SSE 全部挂好后置位。
  window.__ggBridgeReady = true;

  // ---------- SSE 订阅（Go 侧补丁 + 命令复用同一数据通道） ----------
  try {
    var es = new EventSource('/gg/sse');
    es.onopen = function () {
      // 连上瞬间对齐一次：连上前的更新可能未入队，拉当前全量补丁应用。
      fetch('/gg/patch').then(function (r) { return r.text(); })
        .then(function (html) { applyPatch(html); })
        .catch(function () {});
    };
    es.onmessage = function (e) {
      var d = e.data;
      // 命令帧：JSON {cmd:"layout", nonce:"..."}；补丁帧：HTML（以 < 开头）。
      if (d && d.charAt(0) === '{') {
        try {
          var cmd = JSON.parse(d);
          if (cmd.cmd === 'layout') { postLayout(cmd.nonce); return; }
        } catch (err) { /* 非法 JSON 按补丁处理 */ }
      }
      applyPatch(d);
    };
  } catch (e) { /* 不支持 SSE 时静默降级：POST 响应已覆盖用户交互路径 */ }
})();
`

// init 把桥脚本注册进 render 包（HTML 内联），避免页面依赖独立子资源请求。
func init() { render.RegisterBridge(BridgeJS) }
