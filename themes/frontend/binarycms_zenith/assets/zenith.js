(function(){
  'use strict';

  var isMobile = window.innerWidth < 900;

  /* ── Magnetic Cursor ─────────────────────────────── */
  function initCursor() {
    if (isMobile) return;
    var cursor = document.createElement('div');
    cursor.className = 'zh-cursor';
    var follower = document.createElement('div');
    follower.className = 'zh-cursor-follower';
    document.body.appendChild(cursor);
    document.body.appendChild(follower);

    var mouseX = 0, mouseY = 0;
    var cursorX = 0, cursorY = 0;
    var followerX = 0, followerY = 0;
    
    document.addEventListener('mousemove', function(e) {
      mouseX = e.clientX;
      mouseY = e.clientY;
    });

    function render() {
      // Smooth interpolation for follower
      followerX += (mouseX - followerX) * 0.15;
      followerY += (mouseY - followerY) * 0.15;
      
      // Cursor is instantaneous but we update it here
      cursor.style.left = mouseX + 'px';
      cursor.style.top = mouseY + 'px';
      follower.style.left = followerX + 'px';
      follower.style.top = followerY + 'px';
      
      requestAnimationFrame(render);
    }
    requestAnimationFrame(render);

    // Magnetic Links
    var interactables = document.querySelectorAll('a, button, .zh-btn, .zh-logo');
    interactables.forEach(function(el) {
      el.addEventListener('mouseenter', function() {
        var rect = el.getBoundingClientRect();
        follower.style.setProperty('--mag-w', rect.width + 'px');
        follower.style.setProperty('--mag-h', rect.height + 'px');
        cursor.classList.add('magnetic-hover');
        follower.classList.add('magnetic-hover');
      });
      el.addEventListener('mouseleave', function() {
        cursor.classList.remove('magnetic-hover');
        follower.classList.remove('magnetic-hover');
      });
    });

    document.documentElement.addEventListener('mousedown', function(){
      follower.style.transform = 'translate(-50%, -50%) scale(0.8)';
    });
    document.documentElement.addEventListener('mouseup', function(){
      follower.style.transform = 'translate(-50%, -50%) scale(1)';
    });
  }

  /* ── Cryptographic Scramble ──────────────────────── */
  function initScrambler() {
    var chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*()_+';
    document.querySelectorAll('.zh-scramble').forEach(function(el) {
      el.style.visibility = 'visible'; // Reveal it natively
      var originalText = el.dataset.text || el.textContent;
      if (!el.dataset.text) el.dataset.text = originalText;
      
      var observer = new IntersectionObserver(function(entries) {
        if(entries[0].isIntersecting) {
           observer.disconnect();
           var iteration = 0;
           var interval = setInterval(function() {
             el.textContent = originalText.split('').map(function(letter, index) {
               if(index < iteration) return originalText[index];
               if(originalText[index] === ' ') return ' ';
               return chars[Math.floor(Math.random() * chars.length)];
             }).join('');
             
             if(iteration >= originalText.length) {
               clearInterval(interval);
               el.textContent = originalText;
             }
             iteration += 1/3; // Speed multiplier
           }, 30);
        }
      }, { threshold: 0.2 });
      observer.observe(el);
    });
  }

  /* ── Card Hover Glow ─────────────────────────────── */
  function initCardGlow() {
    if (isMobile) return;
    document.querySelectorAll('.zh-card').forEach(function(card) {
      card.addEventListener('mousemove', function(e) {
        var rect = card.getBoundingClientRect();
        var x = e.clientX - rect.left;
        var y = e.clientY - rect.top;
        card.style.setProperty('--mx', x + 'px');
        card.style.setProperty('--my', y + 'px');
      });
    });
  }

  /* ── Fade Up Animations ──────────────────────────── */
  function initFadeUp() {
    var els = document.querySelectorAll('.zh-fade-up');
    var observer = new IntersectionObserver(function(entries) {
      entries.forEach(function(entry) {
        if (entry.isIntersecting) {
          entry.target.classList.add('visible');
          observer.unobserve(entry.target);
        }
      });
    }, { threshold: 0.1 });
    els.forEach(function(el) { observer.observe(el); });
  }

  function init() {
    initCursor();
    initScrambler();
    initCardGlow();
    initFadeUp();
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }

})();
