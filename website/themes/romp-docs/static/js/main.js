(function () {
  // Mobile nav toggle
  var toggle = document.getElementById('nav-toggle');
  var links = document.getElementById('nav-links');
  if (toggle && links) {
    toggle.addEventListener('click', function () {
      var open = links.classList.toggle('open');
      toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
    });
  }

  // Install tabs — roving tabindex so the tablist is one stop, arrows move within it.
  document.querySelectorAll('.install-card').forEach(function (card) {
    var tabs = Array.prototype.slice.call(card.querySelectorAll('.install-tab'));
    if (!tabs.length) return;

    var list = card.querySelector('.install-tabs');
    var glide = card.querySelector('.install-tab-glide');
    var panels = card.querySelector('.install-panels');
    var notes = card.querySelectorAll('.install-note');
    var title = card.querySelector('[data-install-title]');
    var copy = card.querySelector('[data-install-copy]');

    // Park the pill under the active tab. Measured rather than hardcoded so it
    // stays correct once webfonts land and when the labels reflow.
    function slide(tab) {
      if (!glide || !list) return;
      var a = tab.getBoundingClientRect();
      var b = list.getBoundingClientRect();
      var cs = getComputedStyle(list);
      var bl = parseFloat(cs.borderLeftWidth) || 0;
      var bt = parseFloat(cs.borderTopWidth) || 0;
      glide.style.width = a.width + 'px';
      glide.style.height = a.height + 'px';
      glide.style.transform = 'translate(' + (a.left - b.left - bl) + 'px, ' + (a.top - b.top - bt) + 'px)';
      list.setAttribute('data-glide-ready', '');
    }

    // Pin the container to the active panel so the card grows and shrinks
    // instead of sitting at the height of the longest route.
    function resize() {
      if (!panels) return;
      var on = panels.querySelector('.install-panel.active');
      if (!on) return;
      panels.style.height = on.offsetHeight + 'px';
      panels.setAttribute('data-measured', '');
    }

    function select(tab, focus) {
      var id = tab.getAttribute('data-tab');
      tabs.forEach(function (t) {
        var on = t === tab;
        t.classList.toggle('active', on);
        t.setAttribute('aria-selected', on ? 'true' : 'false');
        t.tabIndex = on ? 0 : -1;
      });
      card.querySelectorAll('.install-panel').forEach(function (p) {
        p.classList.toggle('active', p.getAttribute('data-panel') === id);
      });
      notes.forEach(function (n) {
        n.classList.toggle('active', n.getAttribute('data-note') === id);
      });

      var active = card.querySelector('.install-panel.active');
      if (active) {
        if (title) title.textContent = active.getAttribute('data-title') || '';
        if (copy) copy.setAttribute('data-copy', active.getAttribute('data-copy') || '');
      }

      slide(tab);
      resize();
      if (focus) tab.focus();
    }

    select(card.querySelector('.install-tab.active') || tabs[0], false);

    var frame;
    function remeasure() {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(function () {
        var on = card.querySelector('.install-tab.active');
        if (on) slide(on);
        resize();
      });
    }
    window.addEventListener('resize', remeasure);
    // Webfonts land after first paint and change both label widths and line heights.
    if (document.fonts && document.fonts.ready) document.fonts.ready.then(remeasure);

    tabs.forEach(function (tab, i) {
      tab.addEventListener('click', function () { select(tab, false); });
      tab.addEventListener('keydown', function (e) {
        var next = null;
        if (e.key === 'ArrowRight') next = tabs[(i + 1) % tabs.length];
        else if (e.key === 'ArrowLeft') next = tabs[(i - 1 + tabs.length) % tabs.length];
        else if (e.key === 'Home') next = tabs[0];
        else if (e.key === 'End') next = tabs[tabs.length - 1];
        if (!next) return;
        e.preventDefault();
        select(next, true);
      });
    });
  });

  // Copy buttons
  document.querySelectorAll('.copy-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var text = btn.getAttribute('data-copy') || '';
      function done() {
        var svg = btn.querySelector('svg');
        btn.classList.add('copied');
        if (svg) {
          var orig = svg.innerHTML;
          svg.innerHTML = '<path d="M20 6L9 17l-5-5"/>';
          setTimeout(function () {
            svg.innerHTML = orig;
            btn.classList.remove('copied');
          }, 1600);
        }
      }
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(done).catch(function () { fallback(); });
      } else {
        fallback();
      }
      function fallback() {
        var ta = document.createElement('textarea');
        ta.value = text;
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        document.body.appendChild(ta);
        ta.select();
        try { document.execCommand('copy'); } catch (e) {}
        document.body.removeChild(ta);
        done();
      }
    });
  });

  // romp watch, reproduced on the page.
  //
  // The binary's dashboard is the product's front door, so the landing page runs
  // the real thing rather than a screenshot: the same phases, the same palette,
  // the same keys. A tiny simulator advances jobs on an accelerated clock and the
  // reader drives the view with tab / arrows / enter, or by clicking.
  //
  // The server-rendered markup is a valid dashboard on its own, so a reader with
  // no JS, or with JS that fails, still sees a correct screen.
  (function () {
    var root = document.getElementById('watch-tui');
    var screen = document.getElementById('watch-screen');
    if (!root || !screen) return;

    var REPO = 'BRO3886/romp';
    // TICK_MS and SPEED are chosen together so exactly one simulated second
    // passes per repaint: an elapsed counter that jumps in fives reads as a
    // stutter, not as time passing.
    var SPEED = 5;               // simulated seconds per real second
    var TICK_MS = 200;
    var WIDTH = 3;               // matches romp.toml `width`
    var CALM = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    // A touch reader has no esc key and no arrows, so the footer stops being a
    // keyboard legend and becomes the controls themselves.
    var coarse = window.matchMedia('(pointer: coarse)');
    var TOUCH = coarse.matches;

    var TITLES = [
      'Synchronize relay device readiness and sender identities',
      'Retry a failed review verification within budget',
      'Bundle the rompify agent skill with the binary',
      'Record session identifiers on finished job rows',
      'Drain in-flight jobs before the watcher exits',
      'Reclaim stale worktrees left by an interrupted run',
      'Reject an under-scoped issue before editing any file',
      'Keep failed verification output out of the job log',
      'Cap the review gate at the configured fix rounds',
      'Assign the authenticated user when a job claims an issue'
    ];
    var HARNESSES = ['codex', 'claude', 'opencode'];
    var VERIFY = ['make build', 'make test'];

    var ICON = {
      claiming: '●', preparing: '●',
      agent: '◆', fixing: '◆',
      verifying: '◇', 're-verifying': '◇',
      reviewing: '◈', 're-reviewing': '◈',
      publishing: '↑', done: '✓', failed: '×'
    };
    var OUTCOME = {
      done: { icon: '✓', cls: 'p-done' },
      blocked: { icon: '!', cls: 'p-verifying' },
      'changes-requested': { icon: '!', cls: 'p-verifying' },
      red: { icon: '×', cls: 'p-failed' },
      timeout: { icon: '×', cls: 'p-failed' }
    };

    // --- state -------------------------------------------------------------
    var sim = 13 * 3600 + 44 * 60;   // simulated wall clock, in seconds
    var wall = Date.now();
    var active = [];
    var history = [];
    var tab = 0;
    var selected = 0;
    var detail = false;
    var draining = false;
    var drainedAt = 0;
    var issueNo = 12;
    var prNo = 28;
    var timer = null;

    function pick(a) { return a[Math.floor(Math.random() * a.length)]; }
    function rand(lo, hi) { return lo + Math.random() * (hi - lo); }
    function pad(n) { return n < 10 ? '0' + n : '' + n; }
    function esc(t) {
      return String(t).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    }
    function span(cls, text) { return '<span class="' + cls + '">' + esc(text) + '</span>'; }

    function clockText(at) {
      var c = Math.floor(at) % 86400;
      return pad(Math.floor(c / 3600)) + ':' + pad(Math.floor(c / 60) % 60) + ':' + pad(c % 60);
    }
    function stampText(at) {
      var c = Math.floor(at) % 86400;
      var day = new Date();
      var months = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
      return pad(day.getDate()) + ' ' + months[day.getMonth()] + '  ' +
        pad(Math.floor(c / 3600)) + ':' + pad(Math.floor(c / 60) % 60);
    }
    function elapsedText(seconds) {
      var s = Math.max(0, Math.floor(seconds));
      var m = Math.floor(s / 60);
      if (!m) return s + 's';
      var h = Math.floor(m / 60);
      if (!h) return m + 'm' + pad(s % 60) + 's';
      return h + 'h' + pad(m % 60) + 'm' + pad(s % 60) + 's';
    }

    // --- simulation --------------------------------------------------------
    // Each job carries its own queue of steps. Nothing coordinates between
    // jobs, which is the point: romp works `width` issues independently.
    function spawn() {
      var builder = pick(HARNESSES);
      var reviewer = pick(HARNESSES);
      var lenses = 4 + Math.floor(Math.random() * 4);
      var verdict = Math.random();
      var job = {
        issue: ++issueNo,
        title: pick(TITLES),
        builder: builder,
        reviewer: reviewer,
        phase: 'claiming',
        detail: '',
        started: sim,
        events: [],
        steps: [],
        nextAt: sim
      };

      function step(hold, phase, detail) {
        job.steps.push({ hold: hold, phase: phase, detail: detail });
      }

      // Each hold is the time spent in the phase before it, so these read as
      // the durations romp actually reports: minutes in the agent, seconds in
      // build, a minute or two under review.
      step(0, 'claiming', job.title);
      step(rand(3, 7), 'preparing', 'refreshing the base branch');
      step(rand(4, 9), 'agent', 'agent working');
      step(rand(120, 260), 'verifying', '1/2 ' + VERIFY[0]);
      step(rand(18, 30), 'verifying', '2/2 ' + VERIFY[1]);
      step(rand(30, 55), 'publishing', 'opening pull request');
      step(rand(5, 9), 'reviewing', 'reviewer working across ' + lenses + ' lenses (read-only)');

      if (verdict < 0.45) {
        // Review found blocking findings: one fix round, then a clean re-review.
        step(rand(90, 150), 'fixing', 'agent addressing review findings (1/2)');
        step(rand(80, 160), 're-verifying', '1/2 ' + VERIFY[0]);
        step(rand(18, 30), 're-verifying', '2/2 ' + VERIFY[1]);
        step(rand(30, 55), 're-reviewing', 'reviewer checking fixes across ' + lenses + ' lenses (read-only)');
        step(rand(70, 120), 'done', 'review approved after 1 fix round');
      } else if (verdict < 0.62) {
        // Verification went red after the fix round and the budget ran out.
        step(rand(90, 150), 'fixing', 'agent addressing review findings (1/2)');
        step(rand(80, 160), 're-verifying', '1/2 ' + VERIFY[0]);
        step(rand(18, 30), 'failed',
          'red: ' + VERIFY.join(' && ') + ' failed after fix round 2/2');
      } else {
        step(rand(90, 160), 'done', 'review approved with no blocking findings');
      }

      active.push(job);
    }

    function advance(job) {
      var next = job.steps.shift();
      job.phase = next.phase;
      job.detail = next.detail;
      job.events.push({ phase: next.phase, at: sim, text: next.detail });
      if (job.events.length > 12) job.events.shift();
      job.nextAt = sim + (job.steps.length ? job.steps[0].hold : 0);
      return next;
    }

    function retire(job) {
      var failed = job.phase === 'failed';
      history.unshift({
        issue: job.issue,
        outcome: failed ? 'red' : 'done',
        started: job.started,
        finished: sim,
        pr: 'https://github.com/' + REPO + '/pull/' + (++prNo),
        session: null,
        builder: job.builder,
        reviewer: job.reviewer,
        detail: failed ? job.detail : ''
      });
      if (history.length > 24) history.pop();
    }

    function tick() {
      var now = Date.now();
      sim += (now - wall) / 1000 * SPEED;
      wall = now;

      for (var i = active.length - 1; i >= 0; i--) {
        var job = active[i];
        while (sim >= job.nextAt && job.steps.length) advance(job);
        if (!job.steps.length && sim >= job.nextAt + 20) {
          retire(job);
          active.splice(i, 1);
        }
      }

      if (draining) {
        if (!active.length && sim > drainedAt + 25) { draining = false; }
      } else if (active.length < WIDTH && Math.random() < 0.03) {
        spawn();
      }

      clampSelection();
      render();
    }

    // --- view --------------------------------------------------------------
    function rows() { return tab === 0 ? active : history; }

    function clampSelection() {
      var count = rows().length;
      if (selected >= count) selected = Math.max(0, count - 1);
      if (!count) detail = false;
    }

    function phaseCell(phase) {
      var cls = 'p-' + String(phase).replace('-', '');
      if (phase === 're-verifying') cls = 'p-reverifying';
      if (phase === 're-reviewing') cls = 'p-rereviewing';
      if (phase === 'claiming' || phase === 'preparing') cls = 'p-idle';
      return { cls: cls, icon: ICON[phase] || '●' };
    }

    function harnessCell(job) {
      var acting = job.phase === 'agent' || job.phase === 'fixing';
      var reviewing = job.phase === 'reviewing' || job.phase === 're-reviewing';
      var html = '<span class="h-builder' + (acting ? ' is-acting' : '') + '">' +
        esc(job.builder.toUpperCase()) + '</span>';
      if (job.reviewer) {
        html += '<span class="tui-arrow"> → </span>' +
          '<span class="h-reviewer' + (reviewing ? ' is-acting' : '') + '">' +
          esc(job.reviewer.toUpperCase()) + '</span>';
      }
      return '<span class="tui-harness">' + html + '</span>';
    }

    function header() {
      var state = draining
        ? '<span class="tui-state is-draining">◌ DRAINING</span>'
        : '<span class="tui-state is-watching">● WATCHING</span>';
      return '<div class="tui-header">' +
        '<span class="tui-logo">ROMP</span>' +
        '<span class="tui-repo">' + esc(REPO) + '</span>' +
        span('tui-meta', active.length + ' active') +
        state + '</div>';
    }

    function tabs() {
      return '<div class="tui-tabs">' +
        '<button type="button" class="tui-tab' + (tab === 0 ? ' is-on' : '') + '" data-tab="0">' +
        'Active ' + active.length + '</button>' +
        '<button type="button" class="tui-tab' + (tab === 1 ? ' is-on' : '') + '" data-tab="1">' +
        'History ' + history.length + '</button></div>';
    }

    function activeRow(job, index) {
      var cell = phaseCell(job.phase);
      return '<div class="tui-row' + (index === selected ? ' is-selected' : '') + '" data-row="' + index + '">' +
        '<div class="tui-row-head">' +
          '<span class="tui-caret">' + (index === selected ? '▸' : '&nbsp;') + '</span>' +
          '<span class="tui-issue">#' + job.issue + '</span>' +
          '<span class="tui-phase ' + cell.cls + '">' + cell.icon + ' ' + esc(job.phase.toUpperCase()) + '</span>' +
          harnessCell(job) +
          span('tui-elapsed', elapsedText(sim - job.started)) +
        '</div>' +
        span('tui-row-title', job.title || 'Untitled issue') +
        span('tui-row-detail', job.detail) +
      '</div>';
    }

    function historyRow(entry, index) {
      var look = OUTCOME[entry.outcome] || OUTCOME.red;
      return '<div class="tui-row tui-row--history' + (index === selected ? ' is-selected' : '') +
        '" data-row="' + index + '">' +
        '<div class="tui-row-head">' +
          '<span class="tui-caret">' + (index === selected ? '▸' : '&nbsp;') + '</span>' +
          '<span class="tui-issue">#' + entry.issue + '</span>' +
          '<span class="tui-phase ' + look.cls + '">' + look.icon + ' ' + esc(entry.outcome.toUpperCase()) + '</span>' +
          '<span class="tui-harness">' + esc(entry.builder + ' → ' + entry.reviewer) + '</span>' +
          span('tui-elapsed', stampText(entry.finished)) +
        '</div></div>';
    }

    function emptyPanel(text) {
      return '<div class="tui-empty">◌ <strong>' + esc(text) + '</strong>' +
        'Watching for issues with the trigger label</div>';
    }

    function dashboard() {
      var list = rows();
      var body = '';
      if (!list.length) {
        body = emptyPanel(tab === 0 ? 'No active jobs' : 'No finished jobs yet');
      } else {
        for (var i = 0; i < Math.min(list.length, tab === 0 ? 3 : 5); i++) {
          body += tab === 0 ? activeRow(list[i], i) : historyRow(list[i], i);
        }
      }
      return tabs() + '<div class="tui-panel">' + body + '</div>' + footer(false);
    }

    function activeDetail(job) {
      var cell = phaseCell(job.phase);
      var body = '<div class="tui-detail-head">' +
        '<span class="tui-issue">#' + job.issue + '</span>' +
        '<span class="tui-phase ' + cell.cls + '">' + cell.icon + ' ' + esc(job.phase.toUpperCase()) + '</span>' +
        '</div>' + span('tui-detail-title', job.title);

      var events = job.events.slice(-7);
      body += '<div class="tui-timeline">';
      for (var i = 0; i < events.length; i++) {
        var look = phaseCell(events[i].phase);
        body += '<div class="tui-event">' +
          span('tui-connector', i === events.length - 1 ? '└' : '├') +
          '<span class="' + look.cls + '">' + look.icon + '</span>' +
          span('tui-event-time', clockText(events[i].at)) +
          span('tui-event-text', events[i].text) +
        '</div>';
      }
      body += '</div>';
      return '<div class="tui-panel">' + body + '</div>' + footer(true);
    }

    function historyDetail(entry) {
      var look = OUTCOME[entry.outcome] || OUTCOME.red;
      var body = '<div class="tui-detail-head">' +
        '<span class="tui-issue">#' + entry.issue + '</span>' +
        '<span class="tui-phase ' + look.cls + '">' + look.icon + ' ' + esc(entry.outcome.toUpperCase()) + '</span>' +
        '</div>';
      var fields = [
        ['Started', stampText(entry.started)],
        ['Finished', stampText(entry.finished)],
        ['Pull request', entry.pr || '—'],
        ['Session', entry.session || '-'],
        ['Builder', entry.builder],
        ['Reviewer', entry.reviewer || 'disabled or unknown']
      ];
      body += '<div class="tui-timeline">';
      for (var i = 0; i < fields.length; i++) {
        body += '<div class="tui-field">' + span('tui-field-label', fields[i][0]) +
          span('tui-field-value', fields[i][1]) + '</div>';
      }
      body += '</div>';
      if (entry.detail) {
        body += '<div class="tui-detail-note ' + look.cls + '">' + esc(entry.detail) + '</div>';
      }
      return '<div class="tui-panel">' + body + '</div>' + footer(true);
    }

    function key(name, label) {
      return '<button type="button" class="tui-key" data-key="' + name + '">' + label + '</button>';
    }

    function btn(name, label, primary) {
      return '<button type="button" class="tui-btn' + (primary ? ' is-primary' : '') +
        '" data-key="' + name + '">' + label + '</button>';
    }

    function footer(inDetail) {
      var html;
      if (TOUCH) {
        html = '<div class="tui-footer tui-footer--touch">';
        if (inDetail) {
          html += btn('esc', '&larr; Back', true) + btn('q', 'Drain');
        } else {
          html += btn('up', '&uarr;', false) + btn('down', '&darr;', false) +
            btn('enter', 'Inspect', true) + btn('q', 'Drain');
        }
      } else {
        html = '<div class="tui-footer">';
        if (inDetail) {
          html += key('esc', 'esc') + ' back ' + key('q', 'q') + ' drain and quit';
        } else {
          html += key('tab', 'tab') + ' switch ' + key('up', '↑/↓') + ' navigate ' +
            key('enter', 'enter') + ' inspect ' + key('q', 'q') + ' drain';
        }
      }
      html += '</div>';
      if (draining) {
        html += '<div class="tui-drain-note">◌ Finishing running jobs. ' +
          (TOUCH ? 'Tap Drain again to stop them.' : 'Press q again to stop them.') + '</div>';
      }
      return html;
    }

    function render() {
      var list = rows();
      var body;
      if (detail && list[selected]) {
        body = tab === 0 ? activeDetail(list[selected]) : historyDetail(list[selected]);
      } else {
        body = dashboard();
      }
      screen.innerHTML = header() + body;
    }

    // --- input -------------------------------------------------------------
    function press(name) {
      var count = rows().length;
      if (name === 'tab' && !detail) { tab = (tab + 1) % 2; selected = 0; }
      else if (name === 'up') { if (selected > 0) selected--; }
      else if (name === 'down') { if (selected + 1 < count) selected++; }
      else if (name === 'enter') { if (count) detail = true; }
      else if (name === 'esc') { detail = false; }
      else if (name === 'q') { draining = true; drainedAt = sim; }
      clampSelection();
      render();
    }

    root.addEventListener('keydown', function (e) {
      // Swallowing Tab would trap a keyboard reader inside the demo, so two
      // exits stay open: Shift+Tab leaves backwards, Escape leaves forwards
      // once the reader is back on the dashboard.
      if (e.key === 'Escape' && !detail) { root.blur(); return; }
      if (e.key === 'Tab' && (e.shiftKey || detail)) return;

      var name = null;
      if (e.key === 'Tab') name = 'tab';
      else if (e.key === 'ArrowUp' || e.key === 'k') name = 'up';
      else if (e.key === 'ArrowDown' || e.key === 'j') name = 'down';
      else if (e.key === 'Enter') name = 'enter';
      else if (e.key === 'Escape' || e.key === 'Backspace') name = 'esc';
      else if (e.key === 'q') name = 'q';
      if (!name) return;
      e.preventDefault();
      press(name);
    });

    // Delegated so a full re-render never strips the handlers, and so the demo
    // is drivable on a touch screen that has no keys to press.
    root.addEventListener('click', function (e) {
      var keyBtn = e.target.closest('[data-key]');
      if (keyBtn) { press(keyBtn.getAttribute('data-key')); root.focus(); return; }
      var tabBtn = e.target.closest('[data-tab]');
      if (tabBtn) {
        tab = Number(tabBtn.getAttribute('data-tab'));
        selected = 0; detail = false;
        clampSelection(); render(); root.focus();
        return;
      }
      var row = e.target.closest('[data-row]');
      if (!row) return;
      var index = Number(row.getAttribute('data-row'));
      // On a mouse, a second click on the row already under the cursor opens it,
      // which is the pointer equivalent of moving to it and pressing enter. On
      // touch there is no cursor to move, so one tap opens.
      if (TOUCH || index === selected) detail = true;
      selected = index;
      clampSelection(); render(); root.focus();
    });

    // --- lifecycle ---------------------------------------------------------
    // Seed enough finished work that History is worth opening on first look.
    for (var past = 0; past < 9; past++) {
      var ok = past % 4 !== 0;
      history.push({
        issue: issueNo - past,
        outcome: ok ? 'done' : (past === 4 ? 'changes-requested' : 'red'),
        started: sim - (past + 1) * 3600 - 2400,
        finished: sim - (past + 1) * 3600,
        pr: 'https://github.com/' + REPO + '/pull/' + (prNo - past),
        session: null,
        builder: pick(HARNESSES),
        reviewer: pick(HARNESSES),
        detail: ok ? '' : 'red: make build && make test failed after fix round: cd apps/api && go test -count=1'
      });
    }
    // Open on a populated dashboard. Waiting for the simulator to fill three
    // slots organically would leave the first ten seconds looking like an idle
    // watcher, which is the one state the reader learns nothing from.
    function seed(steps, age) {
      spawn();
      var job = active[active.length - 1];
      var resume = sim;
      job.started = sim - age;
      sim = job.started;
      for (var i = 0; i < steps && job.steps.length; i++) {
        advance(job);
        sim += age / (steps + 1);
      }
      sim = resume;
      job.nextAt = resume + rand(8, 45);
    }
    seed(6, 430);
    seed(3, 95);
    render();

    var inView = false;
    var visible = !document.hidden;

    function sync() {
      var want = inView && visible && !CALM;
      if (want === !!timer) return;
      if (!want) { clearInterval(timer); timer = null; return; }
      wall = Date.now();
      timer = setInterval(tick, TICK_MS);
    }

    if ('IntersectionObserver' in window) {
      new IntersectionObserver(function (entries) {
        inView = entries[0].isIntersecting;
        sync();
      }, { threshold: 0.2 }).observe(root);
    } else {
      inView = true;
      sync();
    }
    document.addEventListener('visibilitychange', function () {
      visible = !document.hidden;
      sync();
    });

    if (coarse.addEventListener) {
      coarse.addEventListener('change', function (e) { TOUCH = e.matches; render(); });
    }
  })();

  // Scroll reveal. Elements are visible until this arms the hidden state, so a
  // JS failure leaves a fully readable page rather than a blank one.
  (function () {
    var targets = Array.prototype.slice.call(document.querySelectorAll('[data-reveal]'));
    if (!targets.length) return;
    if (!('IntersectionObserver' in window) ||
        window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;

    document.documentElement.setAttribute('data-reveal-armed', '');

    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (!entry.isIntersecting) return;
        var el = entry.target;
        // Siblings revealed in the same frame cascade; a lone element does not wait.
        var group = el.parentElement ? el.parentElement.querySelectorAll(':scope > [data-reveal]') : [el];
        var i = Array.prototype.indexOf.call(group, el);
        el.style.setProperty('--reveal-delay', Math.min(i, 5) * 70 + 'ms');
        el.classList.add('is-in');
        io.unobserve(el);
      });
    }, { rootMargin: '0px 0px -8% 0px', threshold: 0.12 });

    targets.forEach(function (el) { io.observe(el); });
  })();

  // Invert the sticky nav once it overlaps the dark footer.
  var nav = document.getElementById('nav');
  var footer = document.querySelector('.site-footer');
  if (nav && footer && 'IntersectionObserver' in window) {
    var navIO, resizeFrame;
    var watchFooter = function () {
      if (navIO) navIO.disconnect();
      var strip = nav.offsetHeight;
      var below = Math.max(0, window.innerHeight - strip);
      navIO = new IntersectionObserver(function (entries) {
        nav.classList.toggle('nav--dark', entries[0].isIntersecting);
      }, { rootMargin: '0px 0px -' + below + 'px 0px' });
      navIO.observe(footer);
    };
    watchFooter();
    window.addEventListener('resize', function () {
      cancelAnimationFrame(resizeFrame);
      resizeFrame = requestAnimationFrame(watchFooter);
    });
  }
})();
