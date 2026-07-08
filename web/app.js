// AI Gateway Dashboard - 前端逻辑

const BASE = '';

function getToken() {
  return sessionStorage.getItem('admin_token') || '';
}

function api(path, method, body) {
  const headers = { 'Content-Type': 'application/json' };
  const token = getToken();
  if (token) headers['Authorization'] = 'Bearer ' + token;

  const opts = { method, headers };
  if (body) opts.body = JSON.stringify(body);

  return fetch(BASE + path, opts).then(r => {
    if (!r.ok) return r.json().then(e => Promise.reject(e));
    return r.json().catch(() => ({}));
  });
}

// --- Auth ---
function setToken() {
  const t = document.getElementById('adminToken').value.trim();
  if (!t) return;
  sessionStorage.setItem('admin_token', t);
  document.getElementById('authStatus').textContent = '已认证 ✔';
  document.getElementById('adminToken').value = '';
  loadTenants();
}

(function initAuth() {
  if (getToken()) {
    document.getElementById('authStatus').textContent = '已认证 ✔';
  }
})();

// --- Tab ---
function switchTab(name) {
  document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
  document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
  document.querySelector(`.tab:nth-child(${name === 'tenants' ? 1 : name === 'keys' ? 2 : 3})`).classList.add('active');
  document.getElementById('tab-' + name).classList.add('active');

  if (name === 'tenants') loadTenants();
  else if (name === 'keys') loadTenantsForKeys();
  else if (name === 'usage') {}
}

// --- Tenant ---
function loadTenants() {
  api('/admin/v1/tenants', 'GET').then(data => {
    const tbody = document.getElementById('tenantList');
    tbody.innerHTML = (data || []).map(t => `
      <tr>
        <td>${t.id.substring(0,8)}...</td>
        <td>${esc(t.name)}</td>
        <td class="status-${t.status}">${t.status}</td>
        <td>${new Date(t.created_at).toLocaleString()}</td>
        <td><button class="small" onclick="viewKeys('${t.id}')">查看 Keys</button></td>
      </tr>
    `).join('');
  }).catch(e => alert('加载失败: ' + (e?.error?.message || '网络错误')));
}

function createTenant() {
  const name = document.getElementById('tenantName').value.trim();
  if (!name) return alert('请输入租户名称');
  api('/admin/v1/tenants', 'POST', { name }).then(() => {
    document.getElementById('tenantName').value = '';
    loadTenants();
  }).catch(e => alert('创建失败: ' + (e?.error?.message || '网络错误')));
}

function viewKeys(tid) {
  document.getElementById('keyTenant').value = tid;
  switchTab('keys');
  loadTenantKeys(tid);
}

// --- Keys ---
function loadTenantsForKeys() {
  api('/admin/v1/tenants', 'GET').then(data => {
    const sel = document.getElementById('keyTenant');
    sel.innerHTML = '<option value="">选择租户</option>' + (data || []).map(t =>
      `<option value="${t.id}">${esc(t.name)}</option>`
    ).join('');
  });
}

function loadTenantKeys(tid) {
  if (!tid) tid = document.getElementById('keyTenant').value;
  if (!tid) return alert('请先选择租户');
  api('/admin/v1/tenants/' + tid + '/keys', 'GET').then(data => {
    const tbody = document.getElementById('keyList');
    tbody.innerHTML = (data || []).map(k => `
      <tr>
        <td>${k.id.substring(0,8)}...</td>
        <td>${esc(k.name)}</td>
        <td><code>${esc(k.key_prefix)}</code></td>
        <td class="status-${k.status}">${k.status}</td>
        <td>${k.expires_at ? new Date(k.expires_at).toLocaleDateString() : '永不过期'}</td>
        <td>
          <button class="small" onclick="toggleKey('${k.id}','${k.status}')">
            ${k.status === 'active' ? '禁用' : '启用'}
          </button>
          <button class="small danger" onclick="deleteKey('${k.id}')">删除</button>
        </td>
      </tr>
    `).join('');
  });
}

function createKey() {
  const tenantId = document.getElementById('keyTenant').value;
  const name = document.getElementById('keyName').value.trim();
  if (!tenantId || !name) return alert('请选择租户并输入 Key 名称');

  const scopes = {};
  const models = document.getElementById('scopeModels').value.trim();
  const endpoints = document.getElementById('scopeEndpoints').value.trim();
  const rpm = parseInt(document.getElementById('scopeRPM').value) || 0;
  const expires = document.getElementById('keyExpires').value;

  if (models) scopes.allowed_models = models.split(',').map(s => s.trim()).filter(Boolean);
  if (endpoints) scopes.allowed_endpoints = endpoints.split(',').map(s => s.trim()).filter(Boolean);
  scopes.rate_limit_rpm = rpm;

  const body = { name, scopes };
  if (expires) body.expires_at = new Date(expires).toISOString();

  api('/admin/v1/tenants/' + tenantId + '/keys', 'POST', body).then(result => {
    document.getElementById('keyName').value = '';
    // 显示明文 key
    const panel = document.querySelector('#tab-keys .panel:first-child');
    const existing = panel.querySelector('.key-reveal');
    if (existing) existing.remove();
    const div = document.createElement('div');
    div.className = 'key-reveal';
    div.innerHTML = '<strong>API Key（仅此一次显示，请妥善保存）：</strong><br>' + result.key;
    panel.appendChild(div);
    loadTenantKeys(tenantId);
  }).catch(e => alert('创建失败: ' + (e?.error?.message || '网络错误')));
}

function toggleKey(id, currentStatus) {
  const newStatus = currentStatus === 'active' ? 'disabled' : 'active';
  api('/admin/v1/keys/' + id, 'PATCH', { status: newStatus }).then(() => {
    loadTenantKeys(document.getElementById('keyTenant').value);
  }).catch(e => alert('操作失败: ' + (e?.error?.message || '网络错误')));
}

function deleteKey(id) {
  if (!confirm('确认删除此 Key？此操作不可撤销。')) return;
  api('/admin/v1/keys/' + id, 'DELETE').then(() => {
    loadTenantKeys(document.getElementById('keyTenant').value);
  }).catch(e => alert('删除失败: ' + (e?.error?.message || '网络错误')));
}

// --- Usage ---
function queryUsage() {
  const params = new URLSearchParams();
  const tenant = document.getElementById('usageTenant').value.trim();
  const key = document.getElementById('usageKey').value.trim();
  const model = document.getElementById('usageModel').value.trim();
  const start = document.getElementById('usageStart').value;
  const end = document.getElementById('usageEnd').value;
  const groupBy = document.getElementById('usageGroupBy').value;

  if (tenant) params.set('tenant_id', tenant);
  if (key) params.set('key_id', key);
  if (model) params.set('model', model);
  if (start) params.set('start', new Date(start).toISOString());
  if (end) params.set('end', new Date(end).toISOString());
  if (groupBy) params.set('group_by', groupBy);

  api('/admin/v1/usage?' + params.toString(), 'GET').then(data => {
    document.getElementById('usageSummary').innerHTML =
      `<strong>总请求数：</strong>${data.summary.total_requests} &nbsp;|&nbsp;
       <strong>总 Token：</strong>${data.summary.total_tokens}`;

    const tbody = document.getElementById('usageResult');
    tbody.innerHTML = (data.groups || []).map(g => `
      <tr>
        <td>${esc(g.model || '-')}</td>
        <td>${g.requests}</td>
        <td>${g.prompt_tokens}</td>
        <td>${g.completion_tokens}</td>
        <td>${g.total_tokens}</td>
      </tr>
    `).join('');
  }).catch(e => alert('查询失败: ' + (e?.error?.message || '网络错误')));
}

// --- Utilities ---
function esc(s) {
  if (!s) return '';
  const div = document.createElement('div');
  div.textContent = s;
  return div.innerHTML;
}

// 回车提交
document.addEventListener('keydown', function(e) {
  if (e.key === 'Enter' && e.target.tagName === 'INPUT') {
    const fn = e.target.closest('.tab-content.active');
    if (!fn) return;
    if (fn.id === 'tab-tenants') createTenant();
    else if (fn.id === 'tab-keys') createKey();
    else if (fn.id === 'tab-usage') queryUsage();
  }
});

// 初始加载
if (getToken()) loadTenants();
