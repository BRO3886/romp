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

  // Live tail for the terminal. The server-rendered log stays put for no-JS and
  // reduced-motion readers; this only takes over when motion is fine.
  //
  // romp runs `width` jobs at once, so the tail schedules several independent
  // jobs against one shared clock and lets their lines interleave. Each job
  // keeps a colour for the length of its run so a reader can follow one thread
  // through the others.
  (function () {
    var view = document.getElementById('terminal-view');
    var stream = document.getElementById('terminal-stream');
    if (!view || !stream) return;
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;

    var ADJ = ['agile', 'bright', 'calm', 'clever', 'cosmic', 'daring', 'eager', 'gentle',
      'golden', 'happy', 'keen', 'lively', 'lucky', 'merry', 'nimble', 'proud',
      'quick', 'quiet', 'rapid', 'serene', 'steady', 'sunny', 'swift'];
    var NOUN = ['naruto', 'sasuke', 'sakura', 'kakashi', 'itachi', 'jiraiya', 'tsunade',
      'gaara', 'hinata', 'shikamaru', 'neji', 'minato', 'tenten', 'ino', 'konan',
      'kisame', 'deidara', 'zabuza', 'haku', 'asuma', 'iruka'];
    var HARNESS = ['codex', 'claude'];
    var VERIFY = ['go test ./... -count=1', 'make check', 'go build ./... && go test ./...'];

    var WIDTH = 3;              // matches the `width 3` in the header line
    var SPEED = 8;              // displayed seconds per real second

    var slots = [null, null, null];
    var names = [null, null, null];
    var timers = [];
    var running = false;
    var started = 0;
    var issue = 19;
    var pr = 482;
    var clockBase = 21 * 3600 + 47 * 60 + 56;
    var clockFrom = 0;

    function pick(a) { return a[Math.floor(Math.random() * a.length)]; }

    // Two jobs on screen at once must not share a noun, or the reader cannot
    // tell which thread a line belongs to.
    function codename() {
      for (var i = 0; i < 12; i++) {
        var noun = pick(NOUN);
        var clash = names.some(function (n) { return n && n.split('_')[1] === noun; });
        if (!clash) return pick(ADJ) + '_' + noun;
      }
      return pick(ADJ) + '_' + pick(NOUN);
    }
    function rand(lo, hi) { return lo + Math.random() * (hi - lo); }
    function pad(n) { return n < 10 ? '0' + n : '' + n; }

    // One clock for every job, so interleaved timestamps never run backwards.
    function clock() {
      return Math.floor(clockBase + (Date.now() - clockFrom) / 1000 * SPEED) % 86400;
    }
    function stamp() {
      var c = clock();
      return pad(Math.floor(c / 3600)) + ':' + pad(Math.floor(c / 60) % 60) + ':' + pad(c % 60);
    }
    function dur(sec) {
      var m = Math.floor(sec / 60);
      return m ? m + 'm' + (sec % 60) + 's' : sec + 's';
    }
    function esc(t) {
      return String(t).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    }
    function span(cls, text) { return '<span class="' + cls + '">' + esc(text) + '</span>'; }

    function emit(html) {
      var el = document.createElement('div');
      el.className = 'terminal-line';
      el.setAttribute('data-enter', '');
      el.innerHTML = span('t-time', stamp()) + ' ' + html;
      stream.insertBefore(el, caret);
      requestAnimationFrame(function () { el.setAttribute('data-enter-ready', ''); });
      // Prune off-screen history so the DOM and the translate offset stay bounded.
      while (stream.children.length > 26) stream.removeChild(stream.firstElementChild);
      var offset = Math.max(0, stream.offsetHeight - view.clientHeight);
      stream.style.transform = 'translateY(-' + offset + 'px)';
    }

    function after(ms, fn) {
      var id = setTimeout(function () {
        timers.splice(timers.indexOf(id), 1);
        if (running) fn();
      }, ms);
      timers.push(id);
      return id;
    }

    // A job walks its own steps on its own timers; the shared clock does the
    // rest. Nothing here coordinates with the other slots, which is the point.
    function run(slot) {
      var n = ++issue;
      var name = codename();
      names[slot] = name;
      var tag = span('t-codename t-slot-' + slot, '[' + name + ']') + ' ';
      var harness = pick(HARNESS);
      var blocked = Math.random() < 0.22;
      var openedAt = clock();

      var steps = [
        [0, function () { emit('claimed #' + n); }],
        [rand(900, 1700), function () { emit(tag + 'repo BRO3886/romp, issue #' + n); }],
        [rand(1300, 2400), function () { emit(tag + 'worktree ~/Library/Caches/romp/BRO3886-romp/romp-' + n); }],
        [rand(1100, 2000), function () { emit(tag + 'running ' + harness); }],
        [blocked ? rand(3600, 6000) : rand(9000, 17000), function () {
          emit(tag + 'agent took ' + dur(Math.max(1, clock() - openedAt)));
        }]
      ];

      if (blocked) {
        steps.push([rand(1500, 2400), function () {
          emit(tag + 'gap written to .romp/blocked.md');
        }]);
        steps.push([rand(1200, 1900), function () {
          emit(span('t-blocked', '#' + n + ': blocked'));
        }]);
      } else {
        steps.push([rand(3200, 5400), function () {
          emit(tag + 'verify ok (' + pick(VERIFY) + ')');
        }]);
        steps.push([rand(1500, 2400), function () {
          emit(tag + 'PR: ' + span('t-link', 'https://github.com/BRO3886/romp/pull/' + (++pr)));
        }]);
        steps.push([rand(900, 1500), function () {
          emit(span('t-done', '#' + n + ': done'));
        }]);
      }

      var i = 0;
      (function next() {
        if (i >= steps.length) {
          slots[slot] = null;
          names[slot] = null;
          schedule(slot, rand(1800, 5000));
          return;
        }
        var step = steps[i++];
        after(step[0], function () { step[1](); next(); });
      })();
    }

    function schedule(slot, delay) {
      if (!running || slots[slot]) return;
      slots[slot] = true;                 // reserve so the slot is never doubled
      after(delay, function () { run(slot); });
    }

    var caret = document.createElement('div');
    caret.className = 'terminal-line';
    caret.setAttribute('aria-hidden', 'true');
    caret.innerHTML = '<span class="terminal-caret"></span>';

    var inView = false;
    var tabVisible = !document.hidden;
    var armed = false;

    function stop() {
      timers.forEach(clearTimeout);
      timers.length = 0;
      slots = [null, null, null];
      names = [null, null, null];
    }

    function sync() {
      var want = inView && tabVisible;
      if (want === running) return;
      running = want;
      if (!want) { stop(); return; }
      if (!armed) {
        armed = true;
        view.setAttribute('data-live', '');
        stream.setAttribute('data-scrolling', '');
        stream.appendChild(caret);
        stream.style.transform =
          'translateY(-' + Math.max(0, stream.offsetHeight - view.clientHeight) + 'px)';
      }
      // Resuming picks up from the current wall clock rather than replaying the
      // gap the reader spent on another tab.
      clockBase = started ? clock() : clockBase;
      clockFrom = Date.now();
      started = 1;
      for (var i = 0; i < WIDTH; i++) schedule(i, 500 + i * rand(1400, 3000));
    }

    if ('IntersectionObserver' in window) {
      new IntersectionObserver(function (entries) {
        inView = entries[0].isIntersecting;
        sync();
      }, { threshold: 0.25 }).observe(view);
    } else {
      inView = true;
      sync();
    }

    document.addEventListener('visibilitychange', function () {
      tabVisible = !document.hidden;
      sync();
    });
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
