/* Media Gallery frontend — vanilla JavaScript, no dependencies.
 *
 * Reads the current directory from the URL (/gallery/<path>/), fetches the
 * JSON API, renders breadcrumbs / folders / media / pagination, and keeps
 * the browser URL in sync (pushState for navigation, replaceState for
 * sort/page changes) so refresh and back/forward work.
 */
"use strict";

(function () {
  var gridEl = document.getElementById("grid");
  var breadcrumbsEl = document.getElementById("breadcrumbs");
  var paginationEl = document.getElementById("pagination");
  var messageEl = document.getElementById("message");
  var countEl = document.getElementById("count");
  var sortEl = document.getElementById("sort");
  var orderEl = document.getElementById("order");

  var SORTS = ["name", "date", "size"];
  var state = { path: "/", page: 1, sort: "date", order: "desc" };
  var loadSeq = 0; // guards against out-of-order responses

  /* ---------- URL <-> state ---------- */

  function stateFromURL() {
    var p = window.location.pathname;
    if (p === "/gallery" || p === "/gallery/") {
      p = "/";
    } else if (p.indexOf("/gallery/") === 0) {
      p = p.slice("/gallery".length);
    } else {
      p = "/";
    }
    // Decode each segment individually; a malformed escape falls back to raw.
    p = p.split("/").map(function (seg) {
      try { return decodeURIComponent(seg); } catch (e) { return seg; }
    }).join("/");
    if (p !== "/") {
      p = p.replace(/\/+$/, "") || "/";
    }
    var q = new URLSearchParams(window.location.search);
    var page = parseInt(q.get("page"), 10);
    if (!(page >= 1)) page = 1;
    var sort = q.get("sort");
    if (SORTS.indexOf(sort) === -1) sort = "date";
    var order = q.get("order") === "asc" ? "asc" : "desc";
    return { path: p, page: page, sort: sort, order: order };
  }

  function urlFor(st) {
    var enc;
    if (st.path === "/") {
      enc = "/";
    } else {
      enc = st.path.split("/").map(encodeURIComponent).join("/") + "/";
    }
    var q = new URLSearchParams({
      page: String(st.page),
      sort: st.sort,
      order: st.order
    });
    return "/gallery" + enc + "?" + q.toString();
  }

  function apiUrl(st) {
    return "/api/browse?" + new URLSearchParams({
      path: st.path,
      page: String(st.page),
      sort: st.sort,
      order: st.order
    }).toString();
  }

  function mediaURL(vpath) {
    return "/media" + vpath.split("/").map(encodeURIComponent).join("/");
  }

  /* ---------- navigation ---------- */

  function navigate(next, push) {
    state = next;
    if (push) {
      window.history.pushState(state, "", urlFor(state));
    } else {
      window.history.replaceState(state, "", urlFor(state));
    }
    load();
  }

  window.addEventListener("popstate", function () {
    state = stateFromURL();
    load();
  });

  /* ---------- data ---------- */

  function fetchJSON(url) {
    return fetch(url, { headers: { Accept: "application/json" } }).then(function (res) {
      if (!res.ok) {
        return res.json().catch(function () { return {}; }).then(function (body) {
          var err = new Error(body && body.error ? body.error : "Request failed");
          err.status = res.status;
          throw err;
        });
      }
      return res.json();
    });
  }

  function messageFor(status) {
    switch (status) {
      case 401: return "Authentication required. Reload the page to sign in.";
      case 403: return "You are not allowed to view this location.";
      case 404: return "This folder was not found. It may have been moved or deleted.";
      case 400: return "The request was invalid.";
      default: return "The server could not handle the request. Check the application logs.";
    }
  }

  function load() {
    var seq = ++loadSeq;
    gridEl.textContent = "";
    paginationEl.textContent = "";
    messageEl.hidden = false;
    messageEl.className = "message";
    messageEl.textContent = "Loading\u2026";
    countEl.textContent = "";

    fetchJSON(apiUrl(state)).then(function (data) {
      if (seq !== loadSeq) return; // a newer navigation superseded this response
      render(data);
    }, function (err) {
      if (seq !== loadSeq) return;
      gridEl.textContent = "";
      messageEl.hidden = false;
      messageEl.className = "message error";
      messageEl.textContent = err.status ? messageFor(err.status) : "Could not reach the server.";
    });
  }

  /* ---------- rendering ---------- */

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  function render(data) {
    messageEl.hidden = true;
    renderBreadcrumbs(data.breadcrumbs, data.path);
    renderCount(data);
    renderGrid(data);
    renderPagination(data.pagination);
    document.title = (data.path === "/" ? "Media Gallery" : lastSegment(data.path) + " \u2014 Media Gallery");
  }

  function lastSegment(p) {
    var parts = p.split("/").filter(Boolean);
    return parts.length ? parts[parts.length - 1] : "Home";
  }

  function renderBreadcrumbs(crumbs, currentPath) {
    breadcrumbsEl.textContent = "";
    crumbs.forEach(function (c, i) {
      if (i > 0) breadcrumbsEl.appendChild(el("span", "sep", "/"));
      var isCurrent = c.path === currentPath;
      if (isCurrent) {
        breadcrumbsEl.appendChild(el("span", "current", c.name));
      } else {
        var a = el("a", null, c.name);
        a.href = urlFor({ path: c.path, page: 1, sort: state.sort, order: state.order });
        a.addEventListener("click", function (e) {
          if (e.ctrlKey || e.metaKey || e.shiftKey || e.button !== 0) return;
          e.preventDefault();
          navigate({ path: c.path, page: 1, sort: state.sort, order: state.order }, true);
        });
        breadcrumbsEl.appendChild(a);
      }
    });
  }

  function renderCount(data) {
    var parts = [];
    if (data.directories.length) parts.push(data.directories.length + " folder" + (data.directories.length === 1 ? "" : "s"));
    if (data.pagination.total_files) {
      parts.push(data.pagination.total_files + " file" + (data.pagination.total_files === 1 ? "" : "s"));
      if (data.pagination.total_pages > 1) parts.push("page " + data.pagination.page + " of " + data.pagination.total_pages);
    }
    countEl.textContent = parts.length ? parts.join(" \u00b7 ") : "";
  }

  var ICONS = {
    folder: '<svg class="folder-icon" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M10 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-8l-2-2z"/></svg>',
    image: '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M21 19V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2zM8.5 13.5l2.5 3 3.5-4.5 4.5 6H5l3.5-4.5z"/></svg>',
    video: '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M17 10.5V7a1 1 0 0 0-1-1H4a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-3.5l4 4v-11l-4 4z"/></svg>'
  };

  function iconSVG(name) {
    var div = document.createElement("div");
    div.innerHTML = ICONS[name]; // static, trusted markup
    return div.firstChild;
  }

  function renderGrid(data) {
    gridEl.textContent = "";

    if (!data.directories.length && !data.files.length) {
      messageEl.hidden = false;
      messageEl.className = "message";
      messageEl.textContent = data.pagination.total_files === 0 && data.pagination.page > 1
        ? "Nothing on this page."
        : "This folder is empty.";
      return;
    }

    data.directories.forEach(function (dir) {
      var a = el("a", "card folder");
      a.href = urlFor({ path: dir.path, page: 1, sort: state.sort, order: state.order });
      a.dataset.navPath = dir.path;

      var preview = el("div", "preview folder-preview");
      preview.appendChild(iconSVG("folder"));
      var badge = el("span", "badge folder", "Folder");
      preview.appendChild(badge);
      a.appendChild(preview);

      var meta = el("div", "meta");
      meta.appendChild(el("div", "name", dir.name));
      a.appendChild(meta);
      gridEl.appendChild(a);
    });

    data.files.forEach(function (file) {
      var href = mediaURL(file.path);
      var a = el("a", "card media");
      a.href = href;
      a.target = "_blank";
      a.rel = "noreferrer";

      var preview = el("div", "preview");
      if (file.type === "video") {
        var video = document.createElement("video");
        video.preload = "metadata";
        video.src = href + "#t=0.1"; // ask the browser to render the first frame
        video.tabIndex = -1;
        attachFallback(video, "Video \u2014 " + file.name + "\nBrowser cannot preview this video.");
        preview.appendChild(video);
        preview.appendChild(el("span", "badge", "VIDEO"));
      } else {
        var img = document.createElement("img");
        img.loading = "lazy";
        img.decoding = "async";
        img.alt = file.name;
        img.src = href;
        attachFallback(img, "Image \u2014 " + file.name + "\nBrowser cannot preview this image.");
        preview.appendChild(img);
        preview.appendChild(el("span", "badge", "IMG"));
      }
      a.appendChild(preview);

      var meta = el("div", "meta");
      meta.appendChild(el("div", "name", file.name));
      meta.appendChild(el("div", "info", formatSize(file.size) + " \u00b7 " + formatDate(file.modified)));
      a.appendChild(meta);
      gridEl.appendChild(a);
    });
  }

  // If the browser cannot decode the media, show a useful fallback in place.
  function attachFallback(node, text) {
    node.addEventListener("error", function () {
      var parent = node.parentNode;
      if (!parent) return;
      parent.removeChild(node);
      parent.appendChild(el("div", "fallback", text));
    });
  }

  /* ---------- pagination ---------- */

  function pageItems(current, total) {
    if (total <= 7) {
      var all = [];
      for (var i = 1; i <= total; i++) all.push(i);
      return all;
    }
    var set = [1];
    for (var p = current - 1; p <= current + 1; p++) {
      if (p > 1 && p < total && set.indexOf(p) === -1) set.push(p);
    }
    set.push(total);
    var out = [];
    var prev = 0;
    set.sort(function (a, b) { return a - b; });
    set.forEach(function (p) {
      if (prev && p - prev > 1) out.push("\u2026");
      out.push(p);
      prev = p;
    });
    return out;
  }

  function renderPagination(pg) {
    paginationEl.textContent = "";
    if (pg.total_pages <= 1) return;

    function button(label, page, opts) {
      opts = opts || {};
      var b = el("button", null, label);
      if (opts.current) b.setAttribute("aria-current", "page");
      if (opts.disabled) b.disabled = true;
      if (!opts.disabled && !opts.current) {
        b.addEventListener("click", function () {
          navigate({ path: state.path, page: page, sort: state.sort, order: state.order }, false);
        });
      }
      return b;
    }

    paginationEl.appendChild(button("\u2039 Prev", pg.page - 1, { disabled: pg.page <= 1 }));
    pageItems(pg.page, pg.total_pages).forEach(function (item) {
      if (item === "\u2026") {
        paginationEl.appendChild(el("span", "ellipsis", "\u2026"));
      } else {
        paginationEl.appendChild(button(String(item), item, { current: item === pg.page }));
      }
    });
    paginationEl.appendChild(button("Next \u203a", pg.page + 1, { disabled: pg.page >= pg.total_pages }));
  }

  /* ---------- formatting ---------- */

  function formatSize(bytes) {
    if (bytes < 1024) return bytes + " B";
    var units = ["KB", "MB", "GB", "TB"];
    var v = bytes;
    for (var i = 0; i < units.length; i++) {
      v = v / 1024;
      if (v < 1024 || i === units.length - 1) {
        return (v >= 100 ? Math.round(v) : v.toFixed(1)) + " " + units[i];
      }
    }
  }

  function formatDate(iso) {
    var d = new Date(iso);
    if (isNaN(d.getTime())) return "";
    var date = d.toLocaleDateString(undefined, { day: "2-digit", month: "short", year: "numeric" });
    var time = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
    return date + " " + time;
  }

  /* ---------- controls ---------- */

  sortEl.value = state.sort;
  orderEl.textContent = state.order === "asc" ? "\u25b2" : "\u25bc";

  sortEl.addEventListener("change", function () {
    navigate({ path: state.path, page: 1, sort: sortEl.value, order: state.order }, false);
  });

  orderEl.addEventListener("click", function () {
    var next = state.order === "asc" ? "desc" : "asc";
    orderEl.textContent = next === "asc" ? "\u25b2" : "\u25bc";
    navigate({ path: state.path, page: state.page, sort: state.sort, order: next }, false);
  });

  // Folder navigation: intercept plain clicks, keep ctrl/middle-click native.
  gridEl.addEventListener("click", function (e) {
    var link = e.target.closest ? e.target.closest("a.card.folder") : null;
    if (!link) return;
    if (e.ctrlKey || e.metaKey || e.shiftKey || e.altKey || e.button !== 0) return;
    e.preventDefault();
    navigate({ path: link.dataset.navPath, page: 1, sort: state.sort, order: state.order }, true);
  });

  /* ---------- start ---------- */

  state = stateFromURL();
  sortEl.value = state.sort;
  orderEl.textContent = state.order === "asc" ? "\u25b2" : "\u25bc";
  load();
})();
