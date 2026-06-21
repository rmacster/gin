'use strict';

// ---- Auth state -----------------------------------------------------------
const Auth = {
  username: localStorage.getItem('gin_user') || '',
  password: localStorage.getItem('gin_pass') || '',
  header() { return 'Basic ' + btoa(this.username + ':' + this.password); },
  save(u, p) {
    this.username = u; this.password = p;
    localStorage.setItem('gin_user', u);
    localStorage.setItem('gin_pass', p);
  },
  clear() {
    this.username = ''; this.password = '';
    localStorage.removeItem('gin_user');
    localStorage.removeItem('gin_pass');
  },
  has() { return !!this.username && !!this.password; },
};

let ME = null; // current user object

// ---- API helper -----------------------------------------------------------
async function api(method, path, body) {
  const opts = { method, headers: { 'Authorization': Auth.header() } };
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  const res = await fetch('/api' + path, opts);
  let data = null;
  try { data = await res.json(); } catch (e) { /* no body */ }
  if (!res.ok) throw new Error((data && data.error) || res.statusText);
  return data;
}

// ---- View routing ---------------------------------------------------------
const views = ['auth-view', 'admin-view', 'lobby-view', 'game-view'];
function show(id) {
  views.forEach(v => document.getElementById(v).classList.toggle('hidden', v !== id));
  // Outside a game, show the neutral app title; renderGame sets the variant name.
  if (id !== 'game-view') {
    const t = document.getElementById('app-title'); if (t) t.textContent = '🃏 Rummy';
    document.title = 'Rummy';
  }
}
function $(id) { return document.getElementById(id); }

function setWhoami() {
  const box = $('whoami');
  if (ME) {
    box.classList.remove('hidden');
    $('whoami-name').textContent = ME.username + (ME.role === 'admin' ? ' (admin)' : '');
  } else {
    box.classList.add('hidden');
  }
}

async function route() {
  disconnectLobby(); // re-established by loadLobby() when landing in the lobby
  if (!Auth.has()) { ME = null; setWhoami(); show('auth-view'); return; }
  try {
    ME = await api('GET', '/me');
  } catch (e) {
    Auth.clear(); ME = null; setWhoami(); show('auth-view');
    $('auth-msg').textContent = 'Session expired. Please log in.';
    return;
  }
  setWhoami();
  if (ME.role === 'admin') { show('admin-view'); loadAdmin(); }
  else { show('lobby-view'); loadLobby(); }
}

// ---- Auth UI --------------------------------------------------------------
document.querySelectorAll('.tab').forEach(t => t.addEventListener('click', () => {
  document.querySelectorAll('.tab').forEach(x => x.classList.remove('active'));
  t.classList.add('active');
  const isLogin = t.dataset.tab === 'login';
  $('login-form').classList.toggle('hidden', !isLogin);
  $('register-form').classList.toggle('hidden', isLogin);
  $('auth-msg').textContent = '';
}));

$('login-form').addEventListener('submit', async e => {
  e.preventDefault();
  const u = $('login-username').value.trim();
  const p = $('login-password').value;
  Auth.save(u, p);
  try {
    ME = await api('GET', '/me');
    $('auth-msg').textContent = '';
    route();
  } catch (err) {
    Auth.clear();
    $('auth-msg').textContent = err.message;
  }
});

$('register-form').addEventListener('submit', async e => {
  e.preventDefault();
  const body = {
    username: $('reg-username').value.trim(),
    email: $('reg-email').value.trim(),
    password: $('reg-password').value,
  };
  try {
    const r = await fetch('/api/register', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
    });
    const data = await r.json();
    if (!r.ok) throw new Error(data.error || 'failed');
    $('auth-msg').textContent = data.message;
  } catch (err) {
    $('auth-msg').textContent = err.message;
  }
});

$('logout-btn').addEventListener('click', () => {
  closeGame();
  Auth.clear();
  route();
});

// ---- Admin ----------------------------------------------------------------
async function loadAdmin() {
  try {
    const pending = await api('GET', '/admin/pending') || [];
    $('pending-list').innerHTML = '';
    if (pending.length === 0) $('pending-list').innerHTML = '<p class="hint">No pending accounts.</p>';
    pending.forEach(u => {
      const row = el('div', 'list-item');
      row.innerHTML = `<span><b>${esc(u.username)}</b> <span class="meta">${esc(u.email || '')}</span></span>`;
      const actions = el('div', 'row-actions');
      actions.append(
        btn('Approve', 'small', async () => { await api('POST', `/admin/users/${u.id}/approve`); loadAdmin(); }),
        btn('Reject', 'small danger', async () => { await api('POST', `/admin/users/${u.id}/reject`); loadAdmin(); }),
      );
      row.append(actions);
      $('pending-list').append(row);
    });

    const users = await api('GET', '/admin/users') || [];
    $('users-list').innerHTML = '';
    users.forEach(u => {
      const row = el('div', 'list-item');
      const stats = u.role === 'admin' ? 'admin' : `${u.wins}W / ${u.losses}L`;
      const appr = u.approved ? '' : ' <span class="badge">pending</span>';
      row.innerHTML = `<span><b>${esc(u.username)}</b>${appr} <span class="meta">${stats}</span></span>`;
      if (u.role !== 'admin') {
        row.append(btn('Delete', 'small danger', async () => {
          if (confirm(`Delete ${u.username}?`)) { await api('DELETE', `/admin/users/${u.id}`); loadAdmin(); }
        }));
      }
      $('users-list').append(row);
    });
  } catch (err) { alert(err.message); }
}

$('admin-create-form').addEventListener('submit', async e => {
  e.preventDefault();
  try {
    await api('POST', '/admin/users', {
      username: $('ac-username').value.trim(),
      email: $('ac-email').value.trim(),
      password: $('ac-password').value,
    });
    e.target.reset();
    loadAdmin();
  } catch (err) { alert(err.message); }
});

// ---- Lobby ----------------------------------------------------------------
async function loadLobby() {
  connectLobby(); // become "available" for invites while in the lobby
  try {
    const players = await api('GET', '/players') || [];
    const oppBox = $('opponent-list');
    oppBox.innerHTML = '';
    players.filter(p => p.id !== ME.id).forEach(p => {
      const lbl = el('label');
      lbl.innerHTML = `<input type="checkbox" value="${p.id}"> ${esc(p.username)} <span class="meta">(${p.wins}W/${p.losses}L)</span>`;
      oppBox.append(lbl);
    });
    if (oppBox.children.length === 0) oppBox.innerHTML = '<p class="hint">No other players yet — play against robots!</p>';

    const groups = await api('GET', '/groups') || [];
    const gBox = $('groups-list');
    gBox.innerHTML = '';
    if (groups.length === 0) gBox.innerHTML = '<p class="hint">No groups yet.</p>';
    groups.forEach(g => {
      const names = (g.members || []).map(m => esc(m.username)).join(', ');
      const row = el('div', 'list-item');
      row.innerHTML = `<span><b>${esc(g.name)}</b><br><span class="meta">${names}</span></span>`;
      const actions = el('div', 'row-actions');
      actions.append(btn('Invite', 'small', () => inviteGroup(g)));
      actions.append(btn('Add', 'small', async () => {
        const name = prompt('Username to add to ' + g.name + ':');
        if (!name) return;
        const all = await api('GET', '/players');
        const target = all.find(u => u.username === name.trim());
        if (!target) { alert('No such player'); return; }
        await api('POST', `/groups/${g.id}/members`, { user_id: target.id });
        loadLobby();
      }));
      if (g.owner_id === ME.id) actions.append(btn('Delete', 'small ghost', async () => {
        if (!confirm(`Delete the group "${g.name}"? This doesn't affect any games already started.`)) return;
        try { await api('DELETE', `/groups/${g.id}`); loadLobby(); }
        catch (err) { $('lobby-msg').textContent = err.message; }
      }));
      row.append(actions);
      gBox.append(row);
    });

    const games = await api('GET', '/games') || [];
    const gamesBox = $('games-list');
    gamesBox.innerHTML = '';
    if (games.length === 0) gamesBox.innerHTML = '<p class="hint">No games yet.</p>';
    games.forEach(g => {
      const row = el('div', 'list-item');
      const finished = g.status === 'finished';
      const resumable = g.live && !finished;
      const status = finished ? (g.winner_id === ME.id ? 'You won' : 'Finished') : 'In progress';
      const typeLabel = g.game_type === 'rummy' ? 'Rummy' : 'Gin';
      const live = resumable ? ' <span class="badge live">live</span>' : '';
      row.innerHTML = `<span>Game #${g.id} <span class="badge">${typeLabel}</span> <span class="meta">${status}</span>${live}</span>`;
      const actions = el('div', 'row-actions');
      if (resumable) actions.append(btn('Open', 'small', () => openGame(g.id)));
      actions.append(btn('Clear', 'small ghost', async () => {
        if (!finished && !confirm(`Game #${g.id} may still be in progress. Clear it for everyone?`)) return;
        try { await api('DELETE', `/games/${g.id}`); loadLobby(); }
        catch (err) { $('lobby-msg').textContent = err.message; }
      }));
      row.append(actions);
      gamesBox.append(row);
    });
    updateGameHint();
  } catch (err) { $('lobby-msg').textContent = err.message; }
}

$('group-form').addEventListener('submit', async e => {
  e.preventDefault();
  const name = $('group-name').value.trim();
  if (!name) return;
  try { await api('POST', '/groups', { name }); $('group-name').value = ''; loadLobby(); }
  catch (err) { $('lobby-msg').textContent = err.message; }
});

$('game-type').addEventListener('change', updateGameHint);
function updateGameHint() {
  const t = $('game-type').value;
  $('game-hint').textContent = t === 'rummy'
    ? 'Standard Rummy: 2–4 players. Lay melds on the table and empty your hand to win.'
    : 'Gin Rummy: 2–3 players. Knock or go gin to score.';
}

$('create-game-btn').addEventListener('click', async () => {
  const gameType = $('game-type').value;
  const opponents = [...document.querySelectorAll('#opponent-list input:checked')].map(c => +c.value);
  const robots = +$('robot-count').value;
  const target = +$('target-score').value;
  const total = 1 + opponents.length + robots;
  const max = gameType === 'rummy' ? 4 : 3;
  if (total < 2 || total > max) {
    $('lobby-msg').textContent = `Pick 2–${max} players total (you + opponents + robots).`;
    return;
  }
  try {
    const r = await api('POST', '/games', { opponents, robots, target_score: target, game_type: gameType });
    openGame(r.game_id);
  } catch (err) { $('lobby-msg').textContent = err.message; }
});

// inviteGroup creates a game with everyone in the group who is online and free,
// using the current game-type / target-score selectors, and the server pushes
// each of them a join popup. The host drops straight into the game.
async function inviteGroup(g) {
  const gameType = $('game-type').value;
  const target = +$('target-score').value;
  $('lobby-msg').textContent = '';
  try {
    const r = await api('POST', `/groups/${g.id}/invite`, { game_type: gameType, target_score: target });
    openGame(r.game_id);
  } catch (err) { $('lobby-msg').textContent = err.message; }
}

// ---- Lobby presence socket (delivers real-time invites) -------------------
let lobbyWs = null;

function connectLobby() {
  if (lobbyWs && (lobbyWs.readyState === 0 || lobbyWs.readyState === 1)) return; // already connecting/open
  if (!Auth.has() || (ME && ME.role === 'admin')) return;
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const url = `${proto}://${location.host}/lobby` +
    `?u=${encodeURIComponent(Auth.username)}&p=${encodeURIComponent(Auth.password)}`;
  lobbyWs = new WebSocket(url);
  lobbyWs.onmessage = ev => {
    let msg; try { msg = JSON.parse(ev.data); } catch (e) { return; }
    if (msg.type === 'invite') showInvite(msg);
  };
  // Reconnect while the player is still sitting in the lobby.
  lobbyWs.onclose = () => {
    lobbyWs = null;
    if (!$('lobby-view').classList.contains('hidden')) setTimeout(connectLobby, 2000);
  };
}

function disconnectLobby() {
  if (lobbyWs) { lobbyWs.onclose = null; lobbyWs.close(); lobbyWs = null; }
}

// showInvite pops a modal letting the invited player join or dismiss.
function showInvite(msg) {
  const typeName = msg.game_type === 'rummy' ? 'Standard Rummy' : 'Gin Rummy';
  const grp = msg.group ? ` (${esc(msg.group)})` : '';
  const overlay = el('div', 'modal-overlay');
  const box = el('div', 'modal');
  box.innerHTML = `<h3>Game invite</h3>
    <p><b>${esc(msg.from || 'Someone')}</b> invited you to a game of <b>${esc(typeName)}</b>${grp}.</p>`;
  const row = el('div', 'modal-actions');
  const close = () => overlay.remove();
  row.append(btn('Join', '', () => { close(); openGame(msg.game_id); }));
  // Cancel actively leaves the table: the server re-deals to whoever remains
  // (or cancels the game if too few are left).
  row.append(btn('Decline', 'ghost', () => {
    close();
    api('POST', `/games/${msg.game_id}/decline`).catch(() => {});
  }));
  box.append(row);
  overlay.append(box);
  // A new invite supersedes any older popup still on screen.
  document.querySelectorAll('.modal-overlay').forEach(o => o.remove());
  document.body.append(overlay);
}

// ---- Game -----------------------------------------------------------------
let ws = null;
let currentGame = 0;
let selectedCard = null;
let lastState = null;

// Hand display state.
let handSortDir = 'desc';          // 'desc' = high→low, 'asc' = low→high
let prevHand = null;               // last seen hand, to detect a freshly drawn card
let prevHandNumber = null;
const justDrawn = new Set();        // codes to highlight briefly
let highlightTimer = null;
const meldSelection = new Set();   // rummy: cards selected to meld / lay off / discard
let handOrder = [];                // user's manual card arrangement (codes, left→right)
let dragCode = null;               // code currently being dragged
let logHandNumber = null;          // hand the move log currently reflects

const RANK_ORDER = { A: 0, '2': 1, '3': 2, '4': 3, '5': 4, '6': 5, '7': 6, '8': 7, '9': 8, T: 9, J: 10, Q: 11, K: 12 };
const SUIT_ORDER = { C: 0, D: 1, H: 2, S: 3 };

function sortHand(codes) {
  return codes.slice().sort((a, b) => {
    const ra = RANK_ORDER[a[0]], rb = RANK_ORDER[b[0]];
    if (ra !== rb) return handSortDir === 'asc' ? ra - rb : rb - ra;
    return SUIT_ORDER[a[1]] - SUIT_ORDER[b[1]];
  });
}

function updateSortLabel() {
  $('sort-btn').textContent = handSortDir === 'desc' ? 'Sort: High→Low' : 'Sort: Low→High';
}

$('sort-btn').addEventListener('click', () => {
  handSortDir = handSortDir === 'desc' ? 'asc' : 'desc';
  updateSortLabel();
  if (lastState) { handOrder = sortHand(lastState.your_hand || []); renderGame(lastState); }
});

// trackDraw detects a newly drawn card by diffing the hand against the previous
// render, and flags it to be highlighted for a few seconds.
function trackDraw(s) {
  const hand = s.your_hand || [];
  if (s.hand_number !== prevHandNumber) {
    // Fresh deal (or first state): adopt the hand without highlighting anything.
    prevHandNumber = s.hand_number;
    prevHand = hand.slice();
    justDrawn.clear();
    handOrder = sortHand(hand); // a fresh deal starts in sorted order
    return;
  }
  if (prevHand && hand.length === prevHand.length + 1) {
    hand.filter(c => !prevHand.includes(c)).forEach(c => justDrawn.add(c));
    if (highlightTimer) clearTimeout(highlightTimer);
    highlightTimer = setTimeout(() => {
      justDrawn.clear();
      highlightTimer = null;
      if (lastState) renderGame(lastState);
    }, 3000);
  }
  prevHand = hand.slice();
}

function openGame(gameID) {
  disconnectLobby(); // entering a game → no longer "available" in the lobby
  currentGame = gameID;
  selectedCard = null;
  prevHand = null;
  prevHandNumber = null;
  justDrawn.clear();
  handOrder = [];
  dragCode = null;
  if (highlightTimer) { clearTimeout(highlightTimer); highlightTimer = null; }
  updateSortLabel();
  logHandNumber = null;
  meldSelection.clear();
  $('chat-log').innerHTML = '';
  $('log-entries').innerHTML = '';
  show('game-view');
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const url = `${proto}://${location.host}/ws?game=${gameID}` +
    `&u=${encodeURIComponent(Auth.username)}&p=${encodeURIComponent(Auth.password)}`;
  ws = new WebSocket(url);
  ws.onmessage = onWsMessage;
  ws.onclose = () => { $('game-status').textContent = 'Disconnected'; };
  ws.onerror = () => { $('game-status').textContent = 'Connection error'; };
}

function closeGame() {
  if (ws) { ws.onclose = null; ws.close(); ws = null; }
  currentGame = 0;
}

$('leave-game').addEventListener('click', () => { closeGame(); show('lobby-view'); loadLobby(); });

function send(obj) { if (ws && ws.readyState === 1) ws.send(JSON.stringify(obj)); }

function onWsMessage(ev) {
  const msg = JSON.parse(ev.data);
  if (msg.type === 'state') { lastState = msg.state; renderGame(msg.state); }
  else if (msg.type === 'chat') { addChat(msg.from, msg.text, msg.ts); }
  else if (msg.type === 'log') { addLog(msg.actor_id, msg.actor, msg.verb, msg.ts); }
  else if (msg.type === 'error') { addChat('System', msg.error, nowTime()); }
  else if (msg.type === 'closed') {
    closeGame();
    show('lobby-view');
    loadLobby();
    $('lobby-msg').textContent = msg.reason || 'The game was closed.';
  }
}

// Original, geometric caricature per robot — an archetype + colour scheme that
// evokes the character without copying any trademarked artwork.
const ROBOT_FACE = {
  'Bugs Bunny':      { type: 'rabbit', skin: '#b9c2cc', accent: '#f7b6c2', bg: '#dfe7ee' },
  'Mickey Mouse':    { type: 'mouse',  skin: '#33333a', accent: '#e23b3b', bg: '#ffe2c2' },
  'Daffy Duck':      { type: 'duck',   skin: '#262626', accent: '#f59e0b', bg: '#d7e9f5' },
  'Donald Duck':     { type: 'duck',   skin: '#f4f4f4', accent: '#f59e0b', bg: '#cfe3f7' },
  'Homer Simpson':   { type: 'human',  skin: '#ffd23f', bald: true,        bg: '#bfe3c0' },
  'Bart Simpson':    { type: 'human',  skin: '#ffd23f', hair: '#ffd23f', spiky: true, bg: '#cfe6ff' },
  'SpongeBob':       { type: 'sponge', skin: '#ffe14d', accent: '#caa83a', bg: '#bfe9ff' },
  'Scooby Doo':      { type: 'dog',    skin: '#9c6b3f', accent: '#6e4a2c', bg: '#cfe9c2' },
  'Fred Flintstone': { type: 'human',  skin: '#f1c9a5', hair: '#2a2a2a',   bg: '#ffd6a0' },
  'Tom Cat':         { type: 'cat',    skin: '#9fb0c0', accent: '#eef3f7', bg: '#dfe7ee' },
  'Jerry Mouse':     { type: 'mouse',  skin: '#b07a4a', accent: '#e9dcc5', bg: '#efe3cf' },
  'Popeye':          { type: 'human',  skin: '#f1c9a5', hat: 'sailor',     bg: '#cfe3f7' },
  'Yogi Bear':       { type: 'bear',   skin: '#8a5a2b', accent: '#6cc6ff', bg: '#cfe9c2' },
  'Tweety Bird':     { type: 'bird',   skin: '#ffd83b', accent: '#f59e0b', bg: '#cfeaff' },
  'Wile E. Coyote':  { type: 'dog',    skin: '#a98c6b', accent: '#75543a', bg: '#e7d6bf' },
  'Road Runner':     { type: 'bird',   skin: '#7e57c2', accent: '#26c6da', bg: '#e6dcf5' },
  'Porky Pig':       { type: 'pig',    skin: '#f3a9c0', accent: '#d77a93', bg: '#ffe0ea' },
  'Pink Panther':    { type: 'cat',    skin: '#f48fb1', accent: '#fff',    bg: '#ffe0ea' },
  'Bender':          { type: 'robot',  skin: '#9aa7b0', accent: '#cfd8dc', bg: '#dde4e9' },
  'Stewie Griffin':  { type: 'human',  skin: '#f1c9a5', hair: '#e9b04a',   bg: '#ffe7c2' },
  'Peter Griffin':   { type: 'human',  skin: '#f1c9a5', hair: '#5a3a1a', glasses: true, bg: '#cfe3f7' },
  'Rick Sanchez':    { type: 'human',  skin: '#f1c9a5', hair: '#a8cbe6', spiky: true, bg: '#cfeaff' },
  'Morty Smith':     { type: 'human',  skin: '#f1c9a5', hair: '#8a5a2b',   bg: '#ffe9c2' },
  'Patrick Star':    { type: 'star',   skin: '#f48fb1', accent: '#2aa76a', bg: '#bfe9ff' },
  'Velma':           { type: 'human',  skin: '#f1c9a5', hair: '#b5651d', glasses: true, bg: '#ffd9a0' },
  'Shaggy':          { type: 'human',  skin: '#f1c9a5', hair: '#8a7a4a',   bg: '#cfe9c2' },
  'Elmer Fudd':      { type: 'human',  skin: '#f1c9a5', bald: true, hat: 'hunter', bg: '#e7d6bf' },
  'Marvin Martian':  { type: 'alien',  skin: '#2e2e2e', accent: '#2e7d32', bg: '#d7e9f5' },
  'Foghorn Leghorn': { type: 'chicken', skin: '#f7f7f7', accent: '#e23b3b', bg: '#ffe0ea' },
  'Snoopy':          { type: 'dog',    skin: '#fafafa', accent: '#222',    bg: '#cfeaff' },
};

function robotSlug(name) {
  return name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '');
}

// robotAvatarSVG builds an original caricature face for a robot character.
function robotAvatarSVG(name) {
  const c = ROBOT_FACE[name] || { type: 'robot', skin: '#9aa7b0', accent: '#cfd8dc', bg: '#dde4e9' };
  const skin = c.skin, acc = c.accent || '#fff';
  const eyes = `<circle cx="19" cy="25" r="4" fill="#fff"/><circle cx="29" cy="25" r="4" fill="#fff"/>` +
    `<circle cx="19.7" cy="25.6" r="2" fill="#222"/><circle cx="29.7" cy="25.6" r="2" fill="#222"/>`;
  let back = '', head = '', front = '', showEyes = true;

  switch (c.type) {
    case 'rabbit':
      back = `<ellipse cx="18" cy="9" rx="3.5" ry="10" fill="${skin}"/><ellipse cx="30" cy="9" rx="3.5" ry="10" fill="${skin}"/>` +
        `<ellipse cx="18" cy="10" rx="1.5" ry="7" fill="${acc}"/><ellipse cx="30" cy="10" rx="1.5" ry="7" fill="${acc}"/>`;
      head = `<circle cx="24" cy="27" r="13" fill="${skin}"/>`;
      front = `<rect x="22" y="32" width="4" height="4" rx="1" fill="#fff"/>`;
      break;
    case 'mouse':
      back = `<circle cx="15" cy="13" r="7" fill="${skin}"/><circle cx="33" cy="13" r="7" fill="${skin}"/>` +
        `<circle cx="15" cy="13" r="4" fill="${acc}"/><circle cx="33" cy="13" r="4" fill="${acc}"/>`;
      head = `<circle cx="24" cy="27" r="13" fill="${skin}"/>`;
      front = `<circle cx="24" cy="30" r="1.6" fill="#e2738f"/>`;
      break;
    case 'cat':
      back = `<polygon points="13,8 19,20 9,18" fill="${skin}"/><polygon points="35,8 39,18 29,20" fill="${skin}"/>`;
      head = `<circle cx="24" cy="27" r="13" fill="${skin}"/>`;
      front = `<circle cx="24" cy="29" r="1.5" fill="#e2738f"/>`;
      break;
    case 'bear':
      back = `<circle cx="15" cy="13" r="5" fill="${skin}"/><circle cx="33" cy="13" r="5" fill="${skin}"/>`;
      head = `<circle cx="24" cy="27" r="13" fill="${skin}"/><ellipse cx="24" cy="31" rx="6" ry="4.5" fill="#00000018"/>`;
      front = `<circle cx="24" cy="30" r="1.8" fill="#222"/>`;
      break;
    case 'dog':
      back = `<ellipse cx="11" cy="23" rx="4" ry="8" fill="${acc}"/><ellipse cx="37" cy="23" rx="4" ry="8" fill="${acc}"/>`;
      head = `<circle cx="24" cy="27" r="13" fill="${skin}"/>`;
      front = `<circle cx="24" cy="31" r="2.2" fill="#222"/>`;
      break;
    case 'pig':
      back = `<polygon points="15,15 21,20 13,21" fill="${skin}"/><polygon points="33,15 35,21 27,20" fill="${skin}"/>`;
      head = `<circle cx="24" cy="27" r="13" fill="${skin}"/><ellipse cx="24" cy="31" rx="5" ry="3.5" fill="${acc}"/>`;
      front = `<circle cx="22.5" cy="31" r="1" fill="#00000066"/><circle cx="25.5" cy="31" r="1" fill="#00000066"/>`;
      break;
    case 'duck':
      head = `<circle cx="24" cy="24" r="13" fill="${skin}"/>`;
      front = `<ellipse cx="24" cy="33" rx="7" ry="3" fill="${acc}"/>`;
      break;
    case 'bird':
      head = `<circle cx="24" cy="25" r="12" fill="${skin}"/>`;
      front = `<polygon points="24,29 31,31 24,33" fill="${acc}"/>`;
      break;
    case 'chicken':
      back = `<path d="M19 9 q2 -4 3 0 q2 -4 3 0 q2 -4 3 0 v5 h-12 z" fill="${acc}"/>`;
      head = `<circle cx="24" cy="27" r="12" fill="${skin}"/>`;
      front = `<polygon points="24,30 31,32 24,34" fill="#f59e0b"/><path d="M23 34 q1 4 2 0" fill="${acc}"/>`;
      break;
    case 'sponge':
      head = `<rect x="11" y="13" width="26" height="26" rx="4" fill="${skin}" stroke="${acc}" stroke-width="1"/>`;
      front = `<rect x="18" y="33" width="12" height="3.5" rx="1.7" fill="#fff"/>`;
      break;
    case 'star':
      showEyes = false; // star sits lower, so draw its own eyes
      head = `<polygon points="24,9 28,21 41,21 30,29 34,42 24,34 14,42 18,29 7,21 20,21" fill="${skin}"/>`;
      front = `<circle cx="20" cy="24" r="2.6" fill="#fff"/><circle cx="28" cy="24" r="2.6" fill="#fff"/>` +
        `<circle cx="20" cy="24.5" r="1.2" fill="#222"/><circle cx="28" cy="24.5" r="1.2" fill="#222"/>` +
        `<circle cx="24" cy="29" r="1.4" fill="#e2738f"/>`;
      break;
    case 'robot':
      head = `<rect x="12" y="16" width="24" height="22" rx="4" fill="${skin}"/>` +
        `<line x1="24" y1="8" x2="24" y2="16" stroke="${skin}" stroke-width="2"/><circle cx="24" cy="7" r="2.5" fill="${acc}"/>`;
      front = `<rect x="18" y="31" width="12" height="3" rx="1.5" fill="#00000044"/>`;
      break;
    case 'alien':
      back = `<line x1="24" y1="7" x2="24" y2="15" stroke="${skin}" stroke-width="2"/><circle cx="24" cy="6" r="2.5" fill="#e23b3b"/>`;
      head = `<circle cx="24" cy="27" r="12" fill="${skin}"/><path d="M14 24 q10 -13 20 0 z" fill="${acc}"/>`;
      break;
    case 'human':
    default:
      head = `<circle cx="24" cy="27" r="13" fill="${skin}"/>`;
      if (c.hair && !c.bald) {
        back = c.spiky
          ? `<polygon points="12,18 16,5 19,16 24,3 28,16 32,5 36,18" fill="${c.hair}"/>`
          : `<path d="M11 25 a13 13 0 0 1 26 0 q-13 -10 -26 0 z" fill="${c.hair}"/>`;
      }
      break;
  }

  let extra = '';
  if (c.glasses) {
    extra += `<circle cx="19" cy="25" r="5" fill="none" stroke="#222" stroke-width="1.4"/>` +
      `<circle cx="29" cy="25" r="5" fill="none" stroke="#222" stroke-width="1.4"/>` +
      `<line x1="24" y1="25" x2="24" y2="25" stroke="#222" stroke-width="1.4"/>`;
  }
  if (c.hat === 'sailor') extra += `<path d="M15 15 q9 -8 18 0 z" fill="#fff"/><ellipse cx="24" cy="15" rx="11" ry="2.5" fill="#fff"/>`;
  if (c.hat === 'hunter') extra += `<path d="M13 16 q11 -9 22 0 z" fill="#b5651d"/><rect x="11" y="15" width="26" height="3" rx="1.5" fill="#8a4b16"/>`;

  return `<svg class="avatar-svg" viewBox="0 0 48 48" xmlns="http://www.w3.org/2000/svg">` +
    `<rect width="48" height="48" fill="${c.bg || '#e9eef2'}"/>${back}${head}${showEyes ? eyes : ''}${front}${extra}</svg>`;
}

// robotAvatarHTML renders a circular headshot: a real image if one exists at
// robots/<slug>.png, otherwise the original SVG caricature (the <img> removes
// itself on load error, revealing the caricature underneath).
function robotAvatarHTML(name) {
  return `<span class="avatar">${robotAvatarSVG(name)}` +
    `<img class="avatar-img" src="robots/${robotSlug(name)}.png" alt="" loading="lazy" onerror="this.remove()"></span>`;
}

const SUIT = { C: '♣', D: '♦', H: '♥', S: '♠' };

// baseCard renders a plain face-up card element (used for the discard pile too).
function baseCard(code) {
  const rank = code[0], suit = code[1];
  const c = el('div', 'card ' + ((suit === 'D' || suit === 'H') ? 'red' : 'black'));
  c.innerHTML = `<span>${rank === 'T' ? '10' : rank}</span><span class="suit">${SUIT[suit]}</span>`;
  if (justDrawn.has(code)) c.classList.add('just-drawn');
  return c;
}

// handCardEl is a card in the player's own hand: tap to select, drag to
// rearrange. Actions (discard, draw, meld) are driven by the buttons in the
// actions panel — a button-only model that also suits a future arcade cabinet.
function handCardEl(code, selectable, handBox) {
  const c = baseCard(code);
  c.dataset.code = code;
  if (selectedCard === code) c.classList.add('selected');

  makeCardDraggable(c, code, selectable ? () => selectCard(code, handBox) : null);

  return c;
}

// selectCard toggles the selection without rebuilding the hand, so a following
// click can still register as a double-click for quick discard.
function selectCard(code, handBox) {
  selectedCard = (selectedCard === code) ? null : code;
  handBox.querySelectorAll('.card').forEach(el => el.classList.toggle('selected', el.dataset.code === selectedCard));
  refreshDiscardButton();
}

function refreshDiscardButton() {
  const b = $('discard-btn');
  if (!b) return;
  b.disabled = !selectedCard;
  b.textContent = selectedCard ? `Discard ${pretty(selectedCard)}` : 'Select a card to discard';
}

// orderedHand reconciles the manual arrangement with the actual hand: kept cards
// stay put; newly drawn cards are appended on the right.
function orderedHand(hand) {
  let ordered = handOrder.filter(c => hand.includes(c));
  hand.forEach(c => { if (!ordered.includes(c)) ordered.push(c); });
  handOrder = ordered;
  return ordered;
}

function dropBefore(targetCode) {
  if (!dragCode || dragCode === targetCode) return;
  const arr = handOrder.filter(c => c !== dragCode);
  const idx = arr.indexOf(targetCode);
  arr.splice(idx < 0 ? arr.length : idx, 0, dragCode);
  handOrder = arr;
  if (lastState) renderGame(lastState);
}

// ---- Unified pointer drag (mouse + touch) ---------------------------------
// HTML5 drag-and-drop never fires on touchscreens, so cards were undraggable on
// tablets. Pointer Events unify mouse and touch into one code path. A press
// only becomes a drag after the pointer moves past a small threshold, so taps
// still register as normal clicks (select / toggle).
// Selection (tap) and dragging are both resolved here on pointerup, not via a
// separate click event: on touch a tap almost always jitters a few pixels, and
// routing taps through `click` (with drag-suppression) made multi-select drop
// taps. Now any gesture that doesn't land on a real drop target counts as a tap,
// so selecting several cards for a meld always registers.
const DRAG_THRESHOLD = 12; // px of finger movement before a press becomes a drag
let dragGhost = null;      // floating clone that follows the pointer
// `press` tracks the in-progress pointer gesture. The move/up/cancel handlers
// live on `window` (below), not on the card element: on Android a mid-drag
// re-render can detach the card before its pointerup fires, which used to orphan
// the fixed-position ghost on the page. Window always sees the events, and we
// also sweep ghosts by class so a stray clone can never be left behind.
let press = null; // { code, cardEl, pid, startX, startY, onTap, dragging }

// makeCardDraggable arms a hand card for pointer dragging and tapping.
// onTap (optional) fires when the gesture is a tap rather than a real drop.
function makeCardDraggable(cardEl, code, onTap) {
  cardEl.addEventListener('pointerdown', e => {
    if (e.button != null && e.button > 0) return; // primary button / any touch
    if (press) endPress(true); // clear any stuck prior gesture
    sweepGhosts();             // and any orphaned clone from a broken drag
    press = { code, cardEl, pid: e.pointerId, startX: e.clientX, startY: e.clientY, onTap, dragging: false };
  });
}

// sweepGhosts removes every drag clone in the DOM — not just the tracked one —
// so an orphaned ghost can never persist as an on-page artifact.
function sweepGhosts() {
  document.querySelectorAll('.drag-ghost').forEach(g => g.remove());
  dragGhost = null;
}

// endPress clears the current gesture and, when clean, removes all drag visuals.
function endPress(clean) {
  if (press && press.cardEl) press.cardEl.classList.remove('dragging');
  if (clean) {
    sweepGhosts();
    document.querySelectorAll('.card.dragging').forEach(el => el.classList.remove('dragging'));
    document.querySelectorAll('.table-meld.drag-over').forEach(el => el.classList.remove('drag-over'));
  }
  dragCode = null;
  press = null;
}

function onPressMove(e) {
  if (!press || e.pointerId !== press.pid) return;
  if (!press.dragging) {
    if (Math.hypot(e.clientX - press.startX, e.clientY - press.startY) < DRAG_THRESHOLD) return;
    press.dragging = true;
    dragCode = press.code;
    if (press.cardEl) press.cardEl.classList.add('dragging');
    sweepGhosts();
    dragGhost = makeDragGhost(press.cardEl, e.clientX, e.clientY);
  }
  moveDragGhost(e.clientX, e.clientY);
  highlightDropTarget(e.clientX, e.clientY);
  e.preventDefault(); // stop the page from scrolling while a card is in hand
}

function onPressUp(e) {
  if (!press || e.pointerId !== press.pid) return;
  // A drag that performed a real drop is consumed; everything else is a tap.
  const acted = press.dragging && dropAt(e.clientX, e.clientY, press.code);
  const onTap = press.onTap;
  endPress(true);
  if (!acted && onTap) onTap();
}

function onPressCancel(e) {
  if (!press || e.pointerId !== press.pid) return; // interrupted — clean up, no tap
  endPress(true);
}

window.addEventListener('pointermove', onPressMove, { passive: false });
window.addEventListener('pointerup', onPressUp);
window.addEventListener('pointercancel', onPressCancel);
// If the gesture is lost (capture released, tab/app backgrounded), clean up.
window.addEventListener('lostpointercapture', () => { if (press) endPress(true); });
window.addEventListener('blur', () => { if (press) endPress(true); });

function makeDragGhost(cardEl, x, y) {
  const r = cardEl.getBoundingClientRect();
  const g = cardEl.cloneNode(true);
  g.className = 'card drag-ghost' + (cardEl.classList.contains('red') ? ' red' : ' black');
  g.style.width = r.width + 'px';
  g.style.height = r.height + 'px';
  g._offX = x - r.left;
  g._offY = y - r.top;
  document.body.append(g);
  moveDragGhostEl(g, x, y);
  return g;
}
function moveDragGhostEl(g, x, y) {
  g.style.transform = `translate(${x - g._offX}px, ${y - g._offY}px) rotate(4deg)`;
}
function moveDragGhost(x, y) { if (dragGhost) moveDragGhostEl(dragGhost, x, y); }

// dropTargetAt resolves what sits under the pointer. The ghost is
// pointer-events:none so elementFromPoint sees through it.
function dropTargetAt(x, y) {
  const el = document.elementFromPoint(x, y);
  if (!el) return null;
  const meld = el.closest('.table-meld.droppable');
  if (meld) return { kind: 'meld', el: meld };
  const card = el.closest('#your-hand .card');
  if (card) return { kind: 'card', el: card };
  if (el.closest('#your-hand')) return { kind: 'hand' };
  return null;
}

function highlightDropTarget(x, y) {
  document.querySelectorAll('.table-meld.drag-over').forEach(e => e.classList.remove('drag-over'));
  const t = dropTargetAt(x, y);
  if (t && t.kind === 'meld') t.el.classList.add('drag-over');
}

// dropAt performs the drop and returns true if it actually did something. A drag
// released back onto the same card (a jittery tap) does nothing, so the caller
// treats it as a tap and toggles selection instead.
function dropAt(x, y, code) {
  const t = dropTargetAt(x, y);
  if (!t) return false;
  if (t.kind === 'meld') {
    send({ type: 'layoff', card: code, meld_idx: +t.el.dataset.idx });
    meldSelection.clear();
    return true;
  }
  if (t.kind === 'card') {
    if (t.el.dataset.code === code) return false; // dropped on itself → it's a tap
    dropBefore(t.el.dataset.code); // reorder before the card under the pointer
    return true;
  }
  if (t.kind === 'hand') {
    handOrder = handOrder.filter(c => c !== code).concat(code); // dropped past the cards → far right
    if (lastState) renderGame(lastState);
    return true;
  }
  return false;
}

function canDrawNow() {
  return lastState && lastState.turn_user_id === ME.id && lastState.phase === 'draw';
}

const reduceMotion = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

// flipHand animates each hand card from its previous position to its new one
// with an exaggerated lift-and-arc, so rearranging feels lively.
function flipHand(handBox, oldRects) {
  if (reduceMotion || !oldRects.size) return;
  handBox.querySelectorAll('.card').forEach(card => {
    const oldR = oldRects.get(card.dataset.code);
    if (!oldR) return; // a brand-new card (just drawn) — no slide
    const newR = card.getBoundingClientRect();
    const dx = oldR.left - newR.left;
    const dy = oldR.top - newR.top;
    if (Math.abs(dx) < 1 && Math.abs(dy) < 1) return; // didn't move
    const dist = Math.hypot(dx, dy);
    const lift = Math.min(60, 22 + dist * 0.18);          // bigger moves arc higher
    const tilt = (dx === 0 ? (dy > 0 ? 1 : -1) : (dx > 0 ? 1 : -1)) * 9;
    const dur = Math.min(720, 380 + dist * 0.7);
    // Float the moving card above its neighbours for the duration of the arc.
    card.style.position = 'relative';
    card.style.zIndex = '20';
    const anim = card.animate([
      { transform: `translate(${dx}px, ${dy}px) scale(1) rotate(0deg)`,
        boxShadow: '0 4px 14px rgba(0,0,0,.25)', offset: 0 },
      { transform: `translate(${dx * 0.5}px, ${dy * 0.5 - lift}px) scale(1.22) rotate(${tilt}deg)`,
        boxShadow: '0 22px 38px rgba(0,0,0,.55)', offset: 0.55 },
      { transform: 'translate(0,0) scale(1) rotate(0deg)',
        boxShadow: '0 4px 14px rgba(0,0,0,.25)', offset: 1 },
    ], { duration: dur, easing: 'cubic-bezier(.34,1.46,.5,1)' });
    anim.onfinish = () => { card.style.zIndex = ''; card.style.position = ''; };
  });
}

function renderGame(s) {
  if (!s) return;
  // Auto-clear the move log when a new hand (or game) starts.
  if (s.hand_number !== logHandNumber) {
    if (logHandNumber !== null) $('log-entries').innerHTML = '';
    logHandNumber = s.hand_number;
  }
  const myTurn = s.turn_user_id === ME.id;
  const phaseName = { draw: 'Draw', discard: 'Discard', play: 'Play', roundEnd: 'Hand over', gameOver: 'Game over' }[s.phase] || s.phase;
  const typeName = s.game_type === 'rummy' ? 'Rummy' : 'Gin';
  $('app-title').textContent = s.game_type === 'rummy' ? '🃏 Standard Rummy' : '🃏 Gin Rummy';
  document.title = s.game_type === 'rummy' ? 'Standard Rummy' : 'Gin Rummy';
  $('game-status').textContent = `Game #${s.game_id} · ${typeName} · Hand ${s.hand_number} · ${phaseName} · to ${s.target_score}`;

  renderRoster(s);
  renderPiles(s);

  const me = s.players.find(p => p.user_id === ME.id);
  $('you-info').textContent = me ? `${me.username} · Score ${me.score}${me.is_dealer ? ' · dealer' : ''}` : '';

  if (s.game_type === 'rummy') renderRummy(s, myTurn);
  else renderGin(s, myTurn);

  renderDrawOffer(s);
  renderResults(s);
  syncLogHeight();
  refreshFocus();
}

// renderDrawOffer shows the consensual "Offer draw" control during a live hand.
// When every connected human agrees, the server washes the hand with no score.
function renderDrawOffer(s) {
  const box = $('draw-offer');
  box.innerHTML = '';
  const live = s.phase === 'draw' || s.phase === 'discard' || s.phase === 'play';
  const humans = s.draw_offer_humans || 0;
  if (!live || humans < 1) return;
  const votes = s.draw_offer_votes || 0;
  const you = !!s.you_offered_draw;
  const label = you ? `Withdraw draw offer (${votes}/${humans})` : `Offer draw (${votes}/${humans})`;
  box.append(btn(label, you ? 'ghost' : 'ghost', () => send({ type: 'offerDraw' })));
  const hint = el('span', 'hint');
  hint.textContent = humans <= 1
    ? 'Ends this hand now as a no-score draw.'
    : `All players must agree to wash this hand (${votes}/${humans} so far).`;
  box.append(hint);
}

// renderRoster draws the player column (shared by both variants).
function renderRoster(s) {
  const oppBox = $('opponents');
  oppBox.innerHTML = '';
  s.players.forEach(p => {
    const isMe = p.user_id === ME.id;
    const d = el('div', 'opponent' + (p.is_turn ? ' turn' : '') + (isMe ? ' me' : ''));
    let backs = '';
    for (let i = 0; i < p.hand_count; i++) backs += '<div class="mini-back"></div>';
    const avatar = p.is_robot ? robotAvatarHTML(p.username) : '';
    const conn = p.is_robot ? '' : ` <span class="dot ${p.connected ? 'on' : ''}"></span>`;
    const youTag = isMe ? ' <span class="you-tag">(you)</span>' : '';
    d.innerHTML = `${avatar}<div class="name">${esc(p.username)}${youTag}${conn}</div>
      <div class="sub">Score ${p.score}${p.is_dealer ? ' · dealer' : ''}</div>
      <div class="mini-cards">${backs}</div>`;
    oppBox.append(d);
  });
}

// renderPiles draws the stock and discard (shared by both variants).
function renderPiles(s) {
  $('stock-count').textContent = `Stock: ${s.stock_count}`;
  const dp = $('discard-pile');
  dp.innerHTML = '';
  if (s.discard_top) { dp.className = 'pile-card'; dp.appendChild(baseCard(s.discard_top)); }
  else { dp.className = 'card-slot'; }
}

// renderGin draws the Gin Rummy hand and actions.
function renderGin(s, myTurn) {
  const tm = $('table-melds');
  tm.classList.add('hidden');
  tm.innerHTML = '';
  trackDraw(s);
  const handBox = $('your-hand');
  const oldRects = new Map();
  handBox.querySelectorAll('.card').forEach(el => oldRects.set(el.dataset.code, el.getBoundingClientRect()));
  handBox.innerHTML = '';
  const canPickHand = myTurn && s.phase === 'discard';
  orderedHand(s.your_hand || []).forEach(code => handBox.append(handCardEl(code, canPickHand, handBox)));
  if (selectedCard && !(s.your_hand || []).includes(selectedCard)) selectedCard = null;
  flipHand(handBox, oldRects);
  if (s.your_analysis) $('deadwood-info').textContent = `Deadwood: ${s.your_analysis.deadwood}`;
  else $('deadwood-info').textContent = '';
  renderGinActions(s, myTurn);
}

// syncLogHeight keeps the move log no taller than the player column; its entries
// scroll internally.
function syncLogHeight() {
  const col = $('opponents');
  const log = document.getElementById('move-log');
  if (!col || !log) return;
  const h = col.offsetHeight;
  if (h > 0) log.style.height = h + 'px';
}

function renderGinActions(s, myTurn) {
  const box = $('actions');
  box.innerHTML = '';
  if (!myTurn) {
    if (s.phase === 'draw' || s.phase === 'discard') {
      const who = s.players.find(p => p.is_turn);
      box.innerHTML = `<span style="color:#fff">Waiting for ${who ? esc(who.username) : '…'}</span>`;
    }
    if (s.phase === 'roundEnd') box.append(btn('Next hand', '', () => send({ type: 'nextHand' })));
    return;
  }
  if (s.phase === 'draw') {
    box.append(btn('Draw from stock', '', () => send({ type: 'draw', from: 'stock' })));
    if (s.discard_top) box.append(btn(`Take ${pretty(s.discard_top)}`, '', () => send({ type: 'draw', from: 'discard' })));
  } else if (s.phase === 'discard') {
    const canKnock = s.your_analysis && s.your_analysis.can_knock;
    const knockLabel = el('label');
    knockLabel.innerHTML = `<input type="checkbox" id="knock-box" ${canKnock ? '' : 'disabled'}> Knock / Gin`;
    box.append(knockLabel);
    const b = btn(selectedCard ? `Discard ${pretty(selectedCard)}` : 'Select a card to discard', '', () => {
      if (!selectedCard) return;
      const knock = $('knock-box') && $('knock-box').checked;
      send({ type: 'discard', card: selectedCard, knock });
    });
    b.id = 'discard-btn';
    b.disabled = !selectedCard;
    box.append(b);
  } else if (s.phase === 'roundEnd') {
    box.append(btn('Next hand', '', () => send({ type: 'nextHand' })));
  } else if (s.phase === 'gameOver') {
    box.append(btn('Back to lobby', '', () => { closeGame(); show('lobby-view'); loadLobby(); }));
  }
}

// --- Standard Rummy rendering ---

function renderRummy(s, myTurn) {
  const hand = s.your_hand || [];
  meldSelection.forEach(c => { if (!hand.includes(c)) meldSelection.delete(c); });

  renderTableMelds(s, myTurn);

  trackDraw(s);
  const handBox = $('your-hand');
  const oldRects = new Map();
  handBox.querySelectorAll('.card').forEach(el => oldRects.set(el.dataset.code, el.getBoundingClientRect()));
  handBox.innerHTML = '';
  const selectable = myTurn && s.phase === 'play';
  orderedHand(hand).forEach(code => handBox.append(rummyCardEl(code, selectable, handBox)));
  flipHand(handBox, oldRects);

  if (s.your_analysis) $('deadwood-info').textContent = `Cards worth: ${s.your_analysis.remaining}`;
  else $('deadwood-info').textContent = '';

  renderRummyActions(s, myTurn);
}

// rummyCardEl is a hand card with multi-select (for melding), double-click to
// discard, and drag to rearrange.
function rummyCardEl(code, selectable, handBox) {
  const c = baseCard(code);
  c.dataset.code = code;
  if (meldSelection.has(code)) c.classList.add('selected');
  makeCardDraggable(c, code, selectable ? () => rummyToggle(code, handBox) : null);
  return c;
}

function rummyToggle(code, handBox) {
  if (meldSelection.has(code)) meldSelection.delete(code);
  else meldSelection.add(code);
  handBox.querySelectorAll('.card').forEach(el => el.classList.toggle('selected', meldSelection.has(el.dataset.code)));
  if (lastState) { renderTableMelds(lastState, true); renderRummyActions(lastState, true); }
}

// canLayoffClient mirrors the server's lay-off rules so the UI can show valid
// targets and enable the "Add to Meld" button.
function canLayoffClient(meld, code) {
  const cards = meld.cards || [];
  if (cards.includes(code)) return false;
  const rank = c => RANK_ORDER[c[0]], suit = c => SUIT_ORDER[c[1]];
  if (meld.kind === 'set') {
    return cards.length < 4 && rank(code) === rank(cards[0]);
  }
  // run: same suit, extends either end
  if (suit(code) !== suit(cards[0])) return false;
  const ranks = cards.map(rank);
  const lo = Math.min(...ranks), hi = Math.max(...ranks);
  return rank(code) === lo - 1 || rank(code) === hi + 1;
}

// firstLayoffTarget returns the index of the first table meld the card fits, or -1.
function firstLayoffTarget(s, code) {
  if (!s.table) return -1;
  return s.table.findIndex(m => canLayoffClient(m, code));
}

function renderTableMelds(s, myTurn) {
  const box = $('table-melds');
  box.classList.remove('hidden');
  const canPlay = myTurn && s.phase === 'play';
  const oneSel = (canPlay && meldSelection.size === 1) ? [...meldSelection][0] : null;
  box.innerHTML = '<h3>Table melds</h3>';
  const wrap = el('div', 'melds-wrap');
  if (!s.table || s.table.length === 0) {
    wrap.innerHTML = '<span class="hint">No melds yet — lay down a set or run of 3+.</span>';
  } else {
    s.table.forEach((m, i) => {
      const fits = oneSel && canLayoffClient(m, oneSel); // valid lay-off target for the selected card
      const group = el('div', 'table-meld' + (fits ? ' layoff-ready' : '') + (canPlay ? ' droppable' : ''));
      group.dataset.idx = i; // read by the pointer-drag drop handler (dropAt)
      group.innerHTML = miniCardsHTML(m.cards);
      if (fits) {
        group.title = 'Add the selected card to this meld';
        group.addEventListener('click', () => {
          send({ type: 'layoff', card: oneSel, meld_idx: i });
          meldSelection.clear();
        });
      }
      wrap.append(group);
    });
  }
  box.append(wrap);
}

function renderRummyActions(s, myTurn) {
  const box = $('actions');
  box.innerHTML = '';
  if (!myTurn) {
    if (s.phase === 'draw' || s.phase === 'play') {
      const who = s.players.find(p => p.is_turn);
      box.innerHTML = `<span style="color:#fff">Waiting for ${who ? esc(who.username) : '…'}</span>`;
    }
    if (s.phase === 'roundEnd') box.append(btn('Next hand', '', () => send({ type: 'nextHand' })));
    return;
  }
  if (s.phase === 'draw') {
    box.append(btn('Draw from stock', '', () => send({ type: 'draw', from: 'stock' })));
    if (s.discard_top) box.append(btn(`Take ${pretty(s.discard_top)}`, '', () => send({ type: 'draw', from: 'discard' })));
  } else if (s.phase === 'play') {
    const sel = [...meldSelection];
    const meldBtn = btn(sel.length >= 3 ? `Lay meld (${sel.length})` : 'Select 3+ to meld', '', () => {
      if (meldSelection.size >= 3) { send({ type: 'meld', cards: [...meldSelection] }); meldSelection.clear(); }
    });
    meldBtn.disabled = sel.length < 3;
    box.append(meldBtn);

    const target = sel.length === 1 ? firstLayoffTarget(s, sel[0]) : -1;
    const addBtn = btn('Add to Meld', '', () => {
      if (meldSelection.size !== 1) return;
      const card = [...meldSelection][0];
      const t = firstLayoffTarget(s, card);
      if (t >= 0) { send({ type: 'layoff', card, meld_idx: t }); meldSelection.clear(); }
    });
    addBtn.disabled = target < 0;
    box.append(addBtn);

    const discBtn = btn(sel.length === 1 ? `Discard ${pretty(sel[0])}` : 'Select 1 to discard', '', () => {
      if (meldSelection.size === 1) { send({ type: 'discard', card: [...meldSelection][0] }); meldSelection.clear(); }
    });
    discBtn.disabled = sel.length !== 1;
    box.append(discBtn);
    const hint = el('span', 'action-hint');
    hint.textContent = sel.length === 1
      ? (target >= 0
          ? 'Add to Meld puts this card on a highlighted meld — or click/drag it there. Double-click to discard.'
          : 'This card fits no table meld. Select 3+ to lay a meld, or discard.')
      : 'Select 3+ cards to lay a meld, or 1 card to add to a meld / discard.';
    box.append(hint);
  } else if (s.phase === 'roundEnd') {
    box.append(btn('Next hand', '', () => send({ type: 'nextHand' })));
  } else if (s.phase === 'gameOver') {
    box.append(btn('Back to lobby', '', () => { closeGame(); show('lobby-view'); loadLobby(); }));
  }
}

function renderResults(s) {
  const panel = $('results-panel');
  if ((s.phase !== 'roundEnd' && s.phase !== 'gameOver') || !s.results) {
    panel.classList.add('hidden');
    panel.innerHTML = '';
    return;
  }
  if (s.game_type === 'rummy') { renderRummyResults(s, panel); return; }
  panel.classList.remove('hidden');
  let title = 'Hand complete';
  if (s.phase === 'gameOver') {
    const w = s.players.find(p => p.user_id === s.winner_id);
    title = `🏆 ${w ? esc(w.username) : 'Someone'} wins the game!`;
  } else {
    const knocker = s.results.find(r => r.is_knocker);
    if (knocker) title = knocker.gin ? `${esc(knocker.username)} went GIN!` : `${esc(knocker.username)} knocked`;
  }
  let rows = s.results.map(r => {
    const tag = r.undercut ? ' <span class="badge">undercut</span>' : (r.is_knocker ? ' <span class="badge">knock</span>' : '');
    return `<tr><td>${esc(r.username)}${tag}</td><td>${r.deadwood}</td><td>+${r.points}</td><td class="total">${scoreOf(s, r.user_id)}</td></tr>`;
  }).join('');

  // The hand's winner is whoever scored points (knocker, or the undercutter).
  const handWinner = s.results.reduce((best, r) => (r.points > (best ? best.points : 0) ? r : best), null);
  const melds = s.results.map(r => meldsHTML(r, handWinner && r.user_id === handWinner.user_id)).join('');

  panel.innerHTML = `<h3>${title}</h3>
    <table><thead><tr><th>Player</th><th>Deadwood</th><th>This hand</th><th>Total / ${s.target_score}</th></tr></thead>
    <tbody>${rows}</tbody></table>
    <div class="melds-display">${melds}</div>`;
}

// scoreOf returns a player's cumulative match score from the current state.
function scoreOf(s, userId) {
  const p = s.players.find(p => p.user_id === userId);
  return p ? p.score : 0;
}

// renderRummyResults shows who went out and the points scored from opponents' cards.
function renderRummyResults(s, panel) {
  panel.classList.remove('hidden');
  let title = 'Hand complete';
  if (s.phase === 'gameOver') {
    const w = s.players.find(p => p.user_id === s.winner_id);
    title = `🏆 ${w ? esc(w.username) : 'Someone'} wins the game!`;
  } else {
    const out = s.results.find(r => r.went_out);
    const blk = s.results.find(r => r.blocked);
    if (out) title = `${esc(out.username)} went out!`;
    else if (blk) title = `Hand blocked — ${esc(blk.username)} wins on the lowest count`;
    else title = 'Hand blocked — a draw (tie on count)';
  }
  const rows = s.results.map(r => {
    const tag = r.went_out ? ' <span class="badge">went out 👑</span>'
      : (r.blocked ? ' <span class="badge">blocked win 👑</span>' : '');
    const leftover = (r.hand && r.hand.length) ? `<div class="meld-group deadwood-group">${miniCardsHTML(r.hand)}</div>` : '—';
    return `<tr><td>${esc(r.username)}${tag}</td><td>${leftover}</td><td>+${r.points}</td><td class="total">${scoreOf(s, r.user_id)}</td></tr>`;
  }).join('');
  panel.innerHTML = `<h3>${title}</h3>
    <table><thead><tr><th>Player</th><th>Cards left</th><th>This hand</th><th>Total / ${s.target_score}</th></tr></thead>
    <tbody>${rows}</tbody></table>`;
}

// meldsHTML lays out one player's melds (and leftover deadwood) as mini cards.
function meldsHTML(r, isWinner) {
  const groups = (r.melds || []).map(m =>
    `<span class="meld-group ${m.kind}">${miniCardsHTML(m.cards)}</span>`).join('');
  const meldCards = new Set((r.melds || []).flatMap(m => m.cards || []));
  const dead = (r.hand || []).filter(c => !meldCards.has(c));
  const deadGroup = dead.length
    ? `<span class="meld-group deadwood-group" title="Deadwood">${miniCardsHTML(dead)}</span>` : '';
  const crown = isWinner ? '👑 ' : '';
  const cls = isWinner ? 'player-melds winner-melds' : 'player-melds';
  return `<div class="${cls}"><span class="pm-name">${crown}${esc(r.username)}</span>${groups}${deadGroup}</div>`;
}

function miniCardsHTML(codes) {
  return (codes || []).map(code => {
    const rank = code[0], suit = code[1];
    const red = (suit === 'D' || suit === 'H');
    return `<span class="mini-card ${red ? 'red' : 'black'}">${rank === 'T' ? '10' : rank}${SUIT[suit]}</span>`;
  }).join('');
}

// ---- Chat -----------------------------------------------------------------
$('chat-form').addEventListener('submit', e => {
  e.preventDefault();
  const text = $('chat-input').value.trim();
  if (text) { send({ type: 'chat', text }); $('chat-input').value = ''; }
});
function addChat(who, text, ts) {
  const log = $('chat-log');
  const line = el('div', 'line' + (who === 'System' ? ' system' : ''));
  line.innerHTML = `<span class="ts">${ts || ''}</span><span class="who">${esc(who)}:</span> ${esc(text)}`;
  log.append(line);
  log.scrollTop = log.scrollHeight;
}

// addLog appends a move to the move-log panel. The actor reads as "You" for the
// local player, otherwise by name.
function addLog(actorId, actor, verb, ts) {
  const who = (actorId === ME.id) ? 'You' : actor;
  const box = $('log-entries');
  const line = el('div', 'log-line');
  line.innerHTML = `<span class="ts">${ts || ''}</span><span class="who">${esc(who)}</span> ${esc(verb)}`;
  box.append(line);
  box.scrollTop = box.scrollHeight;
}

// nowTime returns the current HH:MM, matching the server's chat timestamp format.
function nowTime() {
  const d = new Date();
  return String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
}

// ---- Small helpers --------------------------------------------------------
function el(tag, cls) { const e = document.createElement(tag); if (cls) e.className = cls; return e; }
function btn(label, cls, onClick) {
  const b = el('button', cls || '');
  b.type = 'button'; b.textContent = label;
  b.addEventListener('click', onClick);
  return b;
}
function esc(s) { return String(s == null ? '' : s).replace(/[&<>"']/g, m => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[m])); }
function pretty(code) { return (code[0] === 'T' ? '10' : code[0]) + SUIT[code[1]]; }

// ---- Single tap on a pile draws (when it's your draw phase) ---------------
// Single tap is reliable on touch and maps cleanly to an arcade button; the
// "Draw from stock" / "Take …" buttons remain as explicit alternatives.
$('stock-pile').addEventListener('click', () => {
  if (canDrawNow()) send({ type: 'draw', from: 'stock' });
});
$('discard-pile').addEventListener('click', () => {
  if (canDrawNow() && lastState.discard_top) send({ type: 'draw', from: 'discard' });
});
$('clear-log').addEventListener('click', () => { $('log-entries').innerHTML = ''; });

// ---- Rotary-encoder / keyboard focus ring ---------------------------------
// The arcade cabinet drives input with a rotary encoder: rotate to move, push
// to activate. We model that as ONE focus ring over every actionable element —
// hand cards, the stock/discard piles, valid lay-off melds, and the action
// buttons. Rotation maps to Left/Right arrows (and the mouse wheel, since many
// encoders enumerate as a wheel); the push button maps to Enter / Space. This
// also makes the whole game fully keyboard-playable on a PC.
let focusIdx = 0;
let lastFocusKey = null; // identity of the focused element, to keep it across re-renders
// The blue focus cursor only makes sense for rotary-encoder / keyboard play, so
// it stays hidden until such input is actually used and disappears again the
// moment the player touches or clicks. Touch devices never fire key/wheel
// events, so they never see the cursor at all.
let kbdMode = false;

function setKbdMode(on) {
  if (kbdMode === on) return;
  kbdMode = on;
  if (inGame()) applyFocus(); // show or hide the cursor immediately
}

function inGame() { return !$('game-view').classList.contains('hidden'); }

// focusables returns the ordered ring of currently-actionable elements.
function focusables() {
  const list = [];
  document.querySelectorAll('#your-hand .card').forEach(el => list.push(el));
  if (canDrawNow()) {
    list.push($('stock-pile'));
    if (lastState && lastState.discard_top) list.push($('discard-pile'));
  }
  document.querySelectorAll('#table-melds .table-meld.layoff-ready').forEach(el => list.push(el));
  document.querySelectorAll('#actions button:not(:disabled), #actions input[type=checkbox]:not(:disabled)')
    .forEach(el => list.push(el));
  document.querySelectorAll('#draw-offer button:not(:disabled)').forEach(el => list.push(el));
  return list;
}

function focusKey(el) {
  if (!el) return null;
  if (el.dataset && el.dataset.code) return 'card:' + el.dataset.code;
  if (el.id) return 'id:' + el.id;
  return 'txt:' + (el.textContent || '').trim();
}

function applyFocus(els) {
  els = els || focusables();
  document.querySelectorAll('.kbd-focus').forEach(e => e.classList.remove('kbd-focus'));
  if (!els.length) { lastFocusKey = null; return; }
  focusIdx = Math.max(0, Math.min(focusIdx, els.length - 1));
  const el = els[focusIdx];
  lastFocusKey = focusKey(el); // tracked even while hidden, so it resumes in place
  if (!kbdMode) return;        // cursor hidden until encoder/keyboard is used
  el.classList.add('kbd-focus');
  if (el.scrollIntoView) el.scrollIntoView({ block: 'nearest', inline: 'nearest' });
}

// refreshFocus re-applies the cursor after a re-render, keeping it on the same
// logical element when that element still exists.
function refreshFocus() {
  if (!inGame()) return;
  const els = focusables();
  if (lastFocusKey) {
    const i = els.findIndex(e => focusKey(e) === lastFocusKey);
    if (i >= 0) focusIdx = i;
  }
  applyFocus(els);
}

function moveFocus(delta) {
  const els = focusables();
  if (!els.length) return;
  focusIdx = (focusIdx + delta + els.length) % els.length;
  applyFocus(els);
}

function activateFocus() {
  const els = focusables();
  const el = els[focusIdx];
  if (!el) return;
  if (el.matches('#your-hand .card')) { tapCard(el); refreshFocus(); }
  else el.click(); // piles, melds, buttons, knock checkbox all have click handlers
}

// tapCard runs the same selection a screen tap would (cards carry no click
// listener — selection is pointer/keyboard driven).
function tapCard(cardEl) {
  const code = cardEl.dataset.code;
  const handBox = $('your-hand');
  if (lastState && lastState.game_type === 'rummy') rummyToggle(code, handBox);
  else selectCard(code, handBox);
}

document.addEventListener('keydown', e => {
  if (!inGame()) return;
  switch (e.key) {
    case 'ArrowRight': case 'ArrowDown': setKbdMode(true); moveFocus(1); e.preventDefault(); break;
    case 'ArrowLeft': case 'ArrowUp': setKbdMode(true); moveFocus(-1); e.preventDefault(); break;
    case 'Enter': case ' ':
      // First press just reveals the cursor; once visible, it activates.
      if (!kbdMode) setKbdMode(true); else activateFocus();
      e.preventDefault(); break;
  }
});
// Many rotary encoders enumerate as a scroll wheel; let it drive the ring too,
// but leave the move-log scrollable.
document.addEventListener('wheel', e => {
  if (!inGame() || e.target.closest('#log-entries')) return;
  setKbdMode(true);
  moveFocus(e.deltaY > 0 ? 1 : -1);
  e.preventDefault();
}, { passive: false });
// Any touch/click means the player isn't using the encoder — hide the cursor.
document.addEventListener('pointerdown', () => setKbdMode(false), true);

// ---- Boot -----------------------------------------------------------------
route();
