const API_BASE = '';

let currentVMs = [];
let selectedAccount = null;
let authEnabled = false;

const els = {};

document.addEventListener('DOMContentLoaded', () => {
  cacheElements();
  bindNavigation();
  bindActions();
  checkAuth();
});

function cacheElements() {
  Object.assign(els, {
    appShell: document.getElementById('app-shell'),
    logoutBtn: document.getElementById('logout-btn'),
    authBadge: document.getElementById('auth-badge'),
    accountList: document.getElementById('account-list'),
    sidebarAccountsMenu: document.getElementById('sidebar-accounts-menu'),
    selectedAccount: document.getElementById('selected-account'),
    vmList: document.getElementById('vm-list'),
    vmCount: document.getElementById('vm-count'),
    runningCount: document.getElementById('running-count'),
    stoppedCount: document.getElementById('stopped-count'),
    logs: document.getElementById('logs'),
    authForm: document.getElementById('auth-settings-form'),
    authEnabled: document.getElementById('auth-enabled'),
    authUsername: document.getElementById('auth-username'),
    authPassword: document.getElementById('auth-password'),
    authSessionHours: document.getElementById('auth-session-hours'),
    authCookieSecure: document.getElementById('auth-cookie-secure'),
    authMessage: document.getElementById('auth-settings-message'),
    configPath: document.getElementById('config-path'),
    configStatus: document.getElementById('config-status'),
    updateCurrentVersion: document.getElementById('update-current-version'),
    updateLatestVersion: document.getElementById('update-latest-version'),
    updateAssetName: document.getElementById('update-asset-name'),
    updateProxyMode: document.getElementById('update-proxy-mode'),
    updateCustomProxy: document.getElementById('update-custom-proxy'),
    updateCustomProxyField: document.getElementById('update-custom-proxy-field'),
    updateMessage: document.getElementById('update-message'),
    saveUpdateProxyBtn: document.getElementById('save-update-proxy-btn')
  });
}

function bindNavigation() {
  document.querySelectorAll('.nav-item').forEach(item => {
    item.addEventListener('click', event => {
      event.preventDefault();
      const section = item.dataset.section;
      document.querySelectorAll('.nav-item').forEach(i => i.classList.remove('active'));
      item.classList.add('active');
      document.querySelectorAll('.section').forEach(s => s.classList.remove('active'));
      document.getElementById(`${section}-section`)?.classList.add('active');
      if (section === 'settings') {
        loadAuthSettings();
        loadConfigStatus();
        loadUpdateStatus();
      }
      if (section === 'dns') {
        loadDNSPage();
      }
    });
  });
}

function bindActions() {
  els.logoutBtn.addEventListener('click', handleLogout);
  document.getElementById('refresh-current-btn').addEventListener('click', refreshVMs);
  document.getElementById('clear-log-btn').addEventListener('click', clearLogs);
  document.getElementById('reload-config-btn').addEventListener('click', reloadConfig);
  document.getElementById('check-update-btn')?.addEventListener('click', () => loadUpdateStatus(true));
  document.getElementById('apply-update-btn')?.addEventListener('click', applyUpdate);
  els.saveUpdateProxyBtn?.addEventListener('click', saveUpdateProxy);
  els.updateProxyMode?.addEventListener('change', updateProxyModeChanged);
  els.authForm.addEventListener('submit', saveAuthSettings);
  document.addEventListener('click', event => {
    // 1. Toggle for Level 2 (instances menu)
    const instToggle = event.target.closest('#instances-toggle-btn');
    if (instToggle) {
      event.stopPropagation();
      event.preventDefault();
      const menu = document.getElementById('sidebar-accounts-menu');
      const icon = instToggle.querySelector('i');
      if (menu) {
        const isCollapsed = menu.classList.toggle('collapsed');
        menu.style.display = isCollapsed ? 'none' : 'flex';
        if (icon) {
          icon.className = isCollapsed ? 'bi bi-plus-lg' : 'bi bi-dash-lg';
        }
      }
      return;
    }

    // 2. Toggle for Level 3 (group accounts)
    const groupToggle = event.target.closest('.sub-nav-group-title .sub-nav-toggle');
    if (groupToggle) {
      event.stopPropagation();
      event.preventDefault();
      const groupTitle = groupToggle.closest('.sub-nav-group-title');
      const group = groupTitle.closest('.sub-nav-group');
      const items = group.querySelector('.sub-nav-group-items');
      const icon = groupToggle.querySelector('i');
      if (items) {
        const isCollapsed = group.classList.toggle('collapsed');
        items.style.display = isCollapsed ? 'none' : 'flex';
        if (icon) {
          icon.className = isCollapsed ? 'bi bi-plus-lg' : 'bi bi-dash-lg';
        }
      }
      return;
    }

    const actionButton = event.target.closest('.action-btn[data-action]');
    if (actionButton) {
      handleVMAction(actionButton);
      return;
    }

    const accountBtn = event.target.closest('.account-pill, .sub-nav-account-item');
    if (accountBtn) {
      const provider = accountBtn.dataset.provider;
      const accountName = accountBtn.dataset.account;
      const group = accountBtn.dataset.group;

      if (accountBtn.classList.contains('sub-nav-account-item')) {
        const instancesNavBtn = document.getElementById('nav-instances-btn');
        if (instancesNavBtn) {
          // Switch tab to instances-section
          instancesNavBtn.click();
        }
      }

      loadAccount({ provider, account: accountName, group });
    }
  });
  initLogPanelCollapse();
}

function initLogPanelCollapse() {
  const logHeader = document.querySelector('.log-header');
  const logPanel = document.querySelector('.log-panel');
  if (!logHeader || !logPanel) return;

  const titleSpan = logHeader.querySelector('span');
  if (titleSpan) {
    titleSpan.style.display = 'inline-flex';
    titleSpan.style.alignItems = 'center';
    titleSpan.style.gap = '8px';

    const icon = document.createElement('i');
    icon.id = 'log-toggle-icon';
    icon.className = 'bi bi-dash-lg';
    icon.style.fontSize = '12px';
    titleSpan.appendChild(icon);
  }

  const savedCollapsed = localStorage.getItem('log-panel-collapsed') === 'true';
  if (savedCollapsed) {
    logPanel.classList.add('collapsed');
    const icon = document.getElementById('log-toggle-icon');
    if (icon) icon.className = 'bi bi-plus-lg';
  }

  logHeader.addEventListener('click', event => {
    if (event.target.closest('#clear-log-btn')) return;

    const isCollapsed = logPanel.classList.toggle('collapsed');
    localStorage.setItem('log-panel-collapsed', isCollapsed);
    const icon = document.getElementById('log-toggle-icon');
    if (icon) {
      icon.className = isCollapsed ? 'bi bi-plus-lg' : 'bi bi-dash-lg';
    }
  });
}

async function checkAuth() {
  try {
    const data = await fetchJSON('/api/auth', { skipAuthRedirect: true });
    authEnabled = Boolean(data.enabled);
    updateAuthUI(data);

    if (data.enabled && !data.authenticated) {
      window.location.replace('/login');
      return;
    }

    unlockApp();
    addLog('已读取本地配置，点击账号可加载机器。', 'info');
    fetchAccounts();
    loadConfigStatus();
    loadUpdateStatus();
  } catch (error) {
    window.location.replace('/login');
  }
}

async function handleLogout() {
  try {
    await fetchJSON('/api/logout', { method: 'POST', skipAuthRedirect: true });
  } catch (error) {
    addLog(`退出登录异常：${error.message}`, 'error');
  }
  window.location.replace('/login');
}

function unlockApp() {
  els.appShell.classList.remove('locked');
}

function updateAuthUI(data) {
  authEnabled = Boolean(data.enabled);
  els.logoutBtn.hidden = !authEnabled;
  els.authBadge.textContent = authEnabled
    ? (data.authenticated ? `已登录：${data.user || 'admin'}` : '需要登录')
    : '认证未开启';
  els.authBadge.classList.toggle('secure', authEnabled && data.authenticated);
}

async function fetchAccounts() {
  els.accountList.innerHTML = '<div class="empty-state compact">正在读取本地账号配置...</div>';

  try {
    const accounts = await fetchJSON('/api/accounts');
    const accountsArr = Array.isArray(accounts) ? accounts : [];
    renderAccounts(accountsArr);
    renderSidebarAccounts(accountsArr);
  } catch (error) {
    els.accountList.innerHTML = `<div class="empty-state compact error">读取失败：${escapeHtml(error.message)}</div>`;
    addLog(`读取账号配置失败：${error.message}`, 'error');
  }
}

function renderAccounts(accounts) {
  if (accounts.length === 0) {
    els.accountList.innerHTML = '<div class="empty-state compact">没有可用账号配置。</div>';
    return;
  }

  els.accountList.innerHTML = accounts.map(account => `
    <button class="account-pill" 
      data-provider="${escapeAttr(account.provider)}"
      data-account="${escapeAttr(account.account)}"
      data-group="${escapeAttr(account.group || account.provider)}"
      type="button">
      <span class="provider-badge">${escapeHtml(account.provider)}</span>
      <span>${escapeHtml(account.account)}</span>
    </button>
  `).join('');
}

function renderSidebarAccounts(accounts) {
  if (!els.sidebarAccountsMenu) return;
  if (accounts.length === 0) {
    els.sidebarAccountsMenu.innerHTML = '';
    return;
  }

  const groups = {};
  accounts.forEach(account => {
    const groupName = account.group || account.provider;
    if (!groups[groupName]) {
      groups[groupName] = [];
    }
    groups[groupName].push(account);
  });

  els.sidebarAccountsMenu.innerHTML = Object.entries(groups).map(([groupName, groupAccounts]) => `
    <div class="sub-nav-group collapsed">
      <div class="sub-nav-group-title">
        <span class="sub-nav-group-title-content">
          <i class="bi bi-folder2-open"></i>
          <span>${escapeHtml(groupName)}</span>
        </span>
        <span class="sub-nav-toggle"><i class="bi bi-plus-lg"></i></span>
      </div>
      <div class="sub-nav-group-items" style="display: none;">
        ${groupAccounts.map(account => `
          <a class="sub-nav-account-item"
            data-provider="${escapeAttr(account.provider)}"
            data-account="${escapeAttr(account.account)}"
            data-group="${escapeAttr(account.group || account.provider)}">
            <i class="bi bi-cloud"></i>
            <span>${escapeHtml(account.account)}</span>
          </a>
        `).join('')}
      </div>
    </div>
  `).join('');
}

async function loadAccount(account) {
  selectedAccount = account;
  els.selectedAccount.textContent = `${account.provider} / ${account.account}`;

  document.querySelectorAll('.account-pill, .sub-nav-account-item').forEach(el => {
    el.classList.toggle('active', el.dataset.provider === account.provider && el.dataset.account === account.account);
  });
  updateDTMonitorVisibility();
  await fetchVMs();
}

async function fetchVMs() {
  if (!selectedAccount) {
    els.vmList.innerHTML = '<div class="empty-state">请选择一个账号加载机器。</div>';
    updateStats([]);
    return;
  }

  els.vmList.innerHTML = '<div class="empty-state">正在加载机器列表...</div>';

  try {
    const vms = await fetchJSON(`/api/vms?provider=${encodeURIComponent(selectedAccount.provider)}&account=${encodeURIComponent(selectedAccount.account)}`);
    currentVMs = Array.isArray(vms) ? vms : [];
    updateStats(currentVMs);
    renderVMList(currentVMs);
    addLog(`已加载 ${selectedAccount.provider}/${selectedAccount.account}，共 ${currentVMs.length} 台机器。`, 'success');
  } catch (error) {
    els.vmList.innerHTML = `<div class="empty-state error">加载失败：${escapeHtml(error.message)}</div>`;
    addLog(`获取 VM 列表失败：${error.message}`, 'error');
  }
}

function updateStats(vms) {
  const running = vms.filter(v => v.status === 'VM running').length;
  const stopped = vms.filter(v => v.status === 'VM deallocated' || v.status === 'VM stopped').length;
  els.vmCount.textContent = vms.length;
  els.runningCount.textContent = running;
  els.stoppedCount.textContent = stopped;
}

function renderVMList(vms) {
  if (vms.length === 0) {
    els.vmList.innerHTML = '<div class="empty-state">当前账号没有加载到 VM 实例。</div>';
    return;
  }

  const groups = groupVMs(vms);
  els.vmList.innerHTML = Object.entries(groups).map(([groupName, groupVMs]) => `
    <section class="provider-group">
      <div class="provider-group-header">
        <h3>${escapeHtml(groupName)}</h3>
        <span>${groupVMs.length} 台</span>
      </div>
      <div class="provider-vm-grid">
        ${groupVMs.map(renderVMCard).join('')}
      </div>
    </section>
  `).join('');
}

function groupVMs(vms) {
  return vms.reduce((groups, vm) => {
    const provider = vm.provider || 'azure';
    const account = vm.accountId || 'default';
    const group = vm.group || provider;
    const key = `${group} / ${provider} / ${account}`;
    groups[key] = groups[key] || [];
    groups[key].push(vm);
    return groups;
  }, {});
}

function renderVMCard(vm) {
  const provider = vm.provider || 'azure';
  const accountId = vm.accountId || 'default';
  const name = vm.name || vm.id || '';
  const id = vm.id || name;
  const status = vm.status || 'Unknown';
  const publicIP = vm.publicIP?.ipAddress || '未分配';
  const publicIPName = vm.publicIP?.name || 'N/A';
  const dnsEnabled = Boolean(vm.dnsEnabled);

  return `
    <article class="vm-card"
      data-provider="${escapeAttr(provider)}"
      data-account-id="${escapeAttr(accountId)}"
      data-id="${escapeAttr(id)}"
      data-name="${escapeAttr(name)}"
      data-status="${escapeAttr(status)}">
      <div class="vm-header">
        <div>
          <span class="vm-kicker">${escapeHtml(provider.toUpperCase())} / ${escapeHtml(accountId)}</span>
          <h3 class="vm-name">${escapeHtml(name)}</h3>
        </div>
        <span class="status-badge status-${getStatusClass(status)}">${escapeHtml(getStatusText(status))}</span>
      </div>

      <dl class="vm-info">
        <div class="info-item wide"><dt>实例名称</dt><dd>${escapeHtml(name)}</dd></div>
        <div class="info-item"><dt>区域</dt><dd>${escapeHtml(getLocationText(vm.location || vm.zone || 'N/A'))}</dd></div>
        <div class="info-item"><dt>规格</dt><dd>${escapeHtml(vm.vmSize || 'N/A')}</dd></div>
        <div class="info-item"><dt>公网 IP</dt><dd class="mono">${escapeHtml(publicIP)}</dd></div>
        <div class="info-item"><dt>公网 IP 名称</dt><dd>${escapeHtml(publicIPName)}</dd></div>
        <div class="info-item"><dt>内网 IP</dt><dd class="mono">${escapeHtml(vm.privateIP || '未分配')}</dd></div>
        <div class="info-item"><dt>资源组 / 项目 / 区间</dt><dd>${escapeHtml(vm.resourceGroup || '-')}</dd></div>
      </dl>

      <div class="vm-options">
        <label class="dns-toggle ${dnsEnabled ? '' : 'disabled'}">
          <input type="checkbox" class="change-ip-dns-toggle" ${dnsEnabled ? '' : 'disabled'}>
          <span>换 IP 后更新 DNS</span>
        </label>
      </div>

      <div class="vm-actions">
        <button class="action-btn start" type="button" data-action="start" ${status === 'VM running' ? 'disabled' : ''}>开机</button>
        <button class="action-btn stop" type="button" data-action="stop" ${status !== 'VM running' ? 'disabled' : ''}>关机</button>
        <button class="action-btn restart" type="button" data-action="restart">重启</button>
        <button class="action-btn change-ip" type="button" data-action="change-ip">换 IP</button>
        ${dnsEnabled ? '<button class="action-btn dns" type="button" data-action="update-dns">更新 DNS</button>' : ''}
        <button class="action-btn dns-bind" type="button" data-action="dns-bind">DNS 绑定</button>
        ${provider === 'oci' ? '<button class="action-btn edit" type="button" data-action="edit">编辑</button>' : ''}
        ${provider === 'oci' ? '<button class="action-btn security-list" type="button" data-action="security-list">安全规则</button>' : ''}
      </div>
    </article>
  `;
}

async function handleVMAction(button) {
  const card = button.closest('.vm-card');
  const action = button.dataset.action;
  const vm = {
    provider: card.dataset.provider,
    accountId: card.dataset.accountId,
    id: card.dataset.id,
    name: card.dataset.name,
    status: card.dataset.status
  };
  const labels = {
    start: '开机',
    stop: '关机',
    restart: '重启',
    'change-ip': '换 IP',
    'update-dns': '更新 DNS',
    edit: '编辑'
  };

  if (action === 'dns-bind') {
    openDNSBindingModal(vm);
    return;
  }
  if (action === 'security-list') {
    openSecurityListModal(vm);
    return;
  }
  if (action === 'edit') {
    openOCIEditModal(vm);
    return;
  }
  if (action === 'start' && vm.status === 'VM running') {
    addLog(`VM ${vm.name} 已经在运行中。`, 'info');
    return;
  }
  if (action === 'stop' && vm.status !== 'VM running') {
    addLog(`VM ${vm.name} 当前不是运行状态。`, 'info');
    return;
  }

  addLog(`正在执行 ${labels[action]}：${vm.accountId}/${vm.name}`, 'info');
  button.disabled = true;

  try {
    const data = await fetchJSON(vmActionURL(vm, action, card), { method: 'POST' });
    if (Array.isArray(data.logs)) data.logs.forEach(log => addLog(log, 'info'));

    if (action === 'change-ip' && data.newIpAddress) {
      addLog(`换 IP 成功，新 IP：${data.newIpAddress}`, 'success');
    } else if (action === 'update-dns' && data.newIpAddress) {
      addLog(`DNS 已按当前 IP 更新：${data.newIpAddress}`, 'success');
    } else {
      addLog(data.message || `${labels[action]}请求已提交。`, 'success');
    }

    setTimeout(() => refreshVM(vm), refreshDelay(action));
  } catch (error) {
    addLog(`${labels[action]}失败：${error.message}`, 'error');
  } finally {
    button.disabled = false;
  }
}

function vmActionURL(vm, action, card) {
  const base = `/api/vm/${encodeURIComponent(vm.provider)}/${encodeURIComponent(vm.accountId)}/${encodeURIComponent(vm.id)}`;
  if (action === 'change-ip') {
    const updateDNS = card?.querySelector('.change-ip-dns-toggle')?.checked === true;
    return `${base}/change-ip?update_dns=${updateDNS ? 'true' : 'false'}`;
  }
  if (action === 'update-dns') return `${base}/update-dns`;
  return `${base}/${action}`;
}

async function refreshVM(vm) {
  try {
    const data = await fetchJSON(`/api/refresh/${encodeURIComponent(vm.provider)}/${encodeURIComponent(vm.accountId)}/${encodeURIComponent(vm.id)}`);
    addLog(`VM ${vm.accountId}/${vm.name} 状态已更新：${getStatusText(data.status)}`, 'info');
    fetchVMs();
  } catch (error) {
    addLog(`刷新 VM 失败：${error.message}`, 'error');
  }
}

function refreshVMs() {
  if (!selectedAccount) {
    addLog('请先选择一个账号。', 'info');
    return;
  }
  addLog(`正在刷新 ${selectedAccount.provider}/${selectedAccount.account} 的 VM 列表...`, 'info');
  fetchVMs();
}

async function loadAuthSettings() {
  try {
    const data = await fetchJSON('/api/settings/auth');
    els.authEnabled.checked = Boolean(data.enabled);
    els.authUsername.value = data.username || '';
    els.authPassword.value = '';
    els.authSessionHours.value = data.session_hours || 12;
    els.authCookieSecure.checked = Boolean(data.cookie_secure);
    els.authMessage.textContent = data.has_password ? '当前已配置密码哈希。' : '当前未配置密码。';
    els.authMessage.className = 'form-message';
  } catch (error) {
    showFormMessage(error.message, true);
  }
}

async function saveAuthSettings(event) {
  event.preventDefault();
  showFormMessage('正在保存...', false);

  try {
    const data = await fetchJSON('/api/settings/auth', {
      method: 'POST',
      body: {
        enabled: els.authEnabled.checked,
        username: els.authUsername.value.trim(),
        password: els.authPassword.value,
        session_hours: Number(els.authSessionHours.value || 12),
        cookie_secure: els.authCookieSecure.checked
      }
    });
    els.authPassword.value = '';
    updateAuthUI({ enabled: data.auth.enabled, authenticated: true, user: data.auth.username });
    showFormMessage('认证配置已保存，并已自动重载生效。', false);
    addLog('认证配置已保存并自动重载。', 'success');
    loadConfigStatus();
  } catch (error) {
    showFormMessage(error.message, true);
  }
}

async function reloadConfig() {
  try {
    await fetchJSON('/api/config/reload', { method: 'POST' });
    addLog('配置文件已手动重载。', 'success');
    showFormMessage('配置文件已重载。', false);
    loadConfigStatus();
    fetchAccounts();
  } catch (error) {
    addLog(`重载配置失败：${error.message}`, 'error');
    showFormMessage(error.message, true);
  }
}

async function loadConfigStatus() {
  try {
    const data = await fetchJSON('/api/config/status');
    els.configPath.textContent = data.path || '-';
    els.configStatus.textContent = data.lastReloadError ? `失败：${data.lastReloadError}` : '正常，等待手动重载';
  } catch (error) {
    els.configStatus.textContent = `读取失败：${error.message}`;
  }
}

async function loadUpdateStatus(isManual = false) {
  if (!els.updateMessage) return;
  if (isManual) {
    els.updateMessage.textContent = '正在检查更新...';
    els.updateMessage.className = 'form-message';
  } else {
    els.updateMessage.textContent = '';
    els.updateMessage.className = 'form-message';
  }
  try {
    const proxy = selectedUpdateProxy();
    const queryParts = [];
    if (isManual) {
      queryParts.push('check=true');
    }
    if (proxy) {
      queryParts.push(`download_proxy=${encodeURIComponent(proxy)}`);
    }
    const query = queryParts.length ? `?${queryParts.join('&')}` : '';
    const data = await fetchJSON(`/api/update/status${query}`);
    els.updateCurrentVersion.textContent = data.currentVersion || '-';
    applyDefaultUpdateProxy(data.downloadProxy || '');
    if (isManual) {
      els.updateLatestVersion.textContent = data.latestVersion || '-';
      els.updateAssetName.textContent = data.assetName || '-';
      if (data.checkError) {
        els.updateMessage.textContent = `检查失败：${data.checkError}`;
        els.updateMessage.className = 'form-message error';
      } else {
        els.updateMessage.textContent = data.updateAvailable ? '发现可用更新。' : '当前已是最新版本。';
        els.updateMessage.className = `form-message ${data.updateAvailable ? 'success' : ''}`;
      }
    }
  } catch (error) {
    if (isManual) {
      els.updateMessage.textContent = `检查失败：${error.message}`;
      els.updateMessage.className = 'form-message error';
    }
  }
}

async function applyUpdate() {
  if (!els.updateMessage) return;
  if (!confirm('确认下载更新并重启程序？')) return;
  els.updateMessage.textContent = '正在下载并安装更新...';
  els.updateMessage.className = 'form-message';
  try {
    const data = await fetchJSON('/api/update/apply', {
      method: 'POST',
      body: { downloadProxy: selectedUpdateProxy() }
    });
    els.updateMessage.textContent = `已安装 ${data.latestVersion || ''}，程序正在重启...`;
    els.updateMessage.className = 'form-message success';
    addLog('程序更新已安装，等待容器自动重启。', 'success');
  } catch (error) {
    els.updateMessage.textContent = `更新失败：${error.message}`;
    els.updateMessage.className = 'form-message error';
  }
}

async function saveUpdateProxy() {
  if (!els.updateMessage) return;
  els.updateMessage.textContent = '正在保存加速源...';
  els.updateMessage.className = 'form-message';
  try {
    const proxy = selectedUpdateProxy();
    const data = await fetchJSON('/api/settings/update', {
      method: 'POST',
      body: { downloadProxy: proxy }
    });
    els.updateMessage.textContent = data.message || '加速源配置已保存。';
    els.updateMessage.className = 'form-message success';
    addLog('加速源配置已保存并重载。', 'success');
  } catch (error) {
    els.updateMessage.textContent = `保存失败：${error.message}`;
    els.updateMessage.className = 'form-message error';
  }
}

function applyDefaultUpdateProxy(proxy) {
  if (!els.updateProxyMode || els.updateProxyMode.dataset.initialized === 'true') return;
  if (proxy === 'https://gh-proxy.com/' || proxy === 'https://gh-proxy.com') {
    els.updateProxyMode.value = 'https://gh-proxy.com/';
  } else if (proxy === '') {
    els.updateProxyMode.value = '';
  } else {
    els.updateProxyMode.value = 'custom';
    els.updateCustomProxy.value = proxy;
  }
  els.updateProxyMode.dataset.initialized = 'true';
  updateProxyModeChanged();
}

function updateProxyModeChanged() {
  if (!els.updateCustomProxyField || !els.updateProxyMode) return;
  els.updateCustomProxyField.hidden = els.updateProxyMode.value !== 'custom';
}

function selectedUpdateProxy() {
  if (!els.updateProxyMode) return '';
  if (els.updateProxyMode.value === 'custom') {
    return els.updateCustomProxy?.value?.trim() || '';
  }
  return els.updateProxyMode.value;
}

function showFormMessage(message, isError) {
  els.authMessage.textContent = message;
  els.authMessage.className = `form-message ${isError ? 'error' : 'success'}`;
}

function refreshDelay(action) {
  if (action === 'restart') return 8000;
  if (action === 'stop' || action === 'change-ip') return 5000;
  return 3000;
}

function getStatusClass(status) {
  if (status === 'VM running') return 'running';
  if (status === 'VM deallocated' || status === 'VM stopped') return 'stopped';
  return 'unknown';
}

function getStatusText(status) {
  if (status === 'VM running') return '运行中';
  if (status === 'VM deallocated' || status === 'VM stopped') return '已停止';
  return status || '未知';
}

function getLocationText(location) {
  const locations = {
    koreacentral: '韩国中部',
    koreasouth: '韩国南部',
    eastasia: '东亚',
    southeastasia: '东南亚',
    centralus: '美国中部',
    eastus: '美国东部',
    westus: '美国西部'
  };
  return locations[location] || location;
}

function cardToAccount(card) {
  return {
    provider: card.dataset.provider,
    account: card.dataset.account,
    group: card.dataset.group
  };
}

async function fetchJSON(path, options = {}) {
  const fetchOptions = {
    method: options.method || 'GET',
    cache: 'no-store',
    headers: { Accept: 'application/json' }
  };

  if (options.body !== undefined) {
    fetchOptions.headers['Content-Type'] = 'application/json';
    fetchOptions.body = JSON.stringify(options.body);
  }

  const response = await fetch(`${API_BASE}${path}`, fetchOptions);
  if (response.status === 401 && !options.skipAuthRedirect) {
    window.location.replace('/login');
    throw new Error('需要登录');
  }

  const text = await response.text();
  let data = {};
  if (text) {
    try {
      data = JSON.parse(text);
    } catch (error) {
      throw new Error(text);
    }
  }

  if (!response.ok || data.error) {
    throw new Error(data.error || `HTTP ${response.status}`);
  }
  return data;
}

function addLog(message, type = 'info') {
  const timestamp = new Date().toLocaleTimeString('zh-CN');
  const entry = document.createElement('div');
  entry.className = `log-entry log-${type}`;
  entry.innerHTML = `<span class="log-time">[${timestamp}]</span>${escapeHtml(message)}`;
  els.logs.prepend(entry);

  while (els.logs.children.length > 80) {
    els.logs.removeChild(els.logs.lastChild);
  }
}

function clearLogs() {
  els.logs.innerHTML = '';
  addLog('日志已清空。', 'info');
}

function escapeHtml(value) {
  return String(value ?? '').replace(/[&<>"']/g, char => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;'
  }[char]));
}

function escapeAttr(value) {
  return escapeHtml(value);
}

// ==================== OCI Data Transfer Monitoring ====================

function updateDTMonitorVisibility() {
  const panel = document.getElementById('dt-monitor-panel');
  if (!panel) return;
  if (selectedAccount && selectedAccount.provider === 'oci') {
    panel.hidden = false;
    document.getElementById('dt-monitor-account-label').textContent = selectedAccount.account;
    loadDTMonitorStatus();
  } else {
    panel.hidden = true;
  }
}

async function loadDTMonitorStatus() {
  if (!selectedAccount || selectedAccount.provider !== 'oci') return;
  const reqAccount = selectedAccount.account;

  // Clear display to prevent showing previous account's data
  document.getElementById('dt-usage-value').textContent = '加载中...';
  document.getElementById('dt-threshold-value').textContent = '-';
  document.getElementById('dt-percentage-value').textContent = '0%';
  const progressFill = document.getElementById('dt-progress-fill');
  if (progressFill) {
    progressFill.style.width = '0%';
    progressFill.className = 'dt-progress-fill';
  }
  document.getElementById('dt-last-update').textContent = '正在获取最新状态...';

  try {
    const data = await fetchJSON(`/api/oci/${encodeURIComponent(reqAccount)}/data-transfer/status`);
    if (selectedAccount && selectedAccount.account === reqAccount) {
      updateDTDisplay(data);
    }
  } catch (error) {
    if (selectedAccount && selectedAccount.account === reqAccount) {
      addLog(`加载数据传输监控状态失败：${error.message}`, 'error');
    }
  }
}

function updateDTDisplay(data) {
  const result = data.lastResult;
  const config = data.config || {};

  // Update stats
  if (result && !result.error) {
    document.getElementById('dt-usage-value').textContent = `${result.usageGB.toFixed(2)} GB`;
    document.getElementById('dt-threshold-value').textContent = `${result.threshold.toFixed(0)} GB`;
    const pct = Math.min(result.percentage, 100);
    document.getElementById('dt-percentage-value').textContent = `${result.percentage.toFixed(1)}%`;
    const progressFill = document.getElementById('dt-progress-fill');
    if (progressFill) {
      progressFill.style.width = `${pct}%`;
      progressFill.className = 'dt-progress-fill' + (result.percentage > 90 ? ' danger' : result.percentage > 70 ? ' warning' : '');
    }

    if (result.queryTime) {
      const t = new Date(result.queryTime);
      document.getElementById('dt-last-update').textContent = `最后更新：${t.toLocaleString('zh-CN')}`;
    }
  } else if (result && result.error) {
    document.getElementById('dt-usage-value').textContent = '查询失败';
    document.getElementById('dt-last-update').textContent = `错误：${result.error}`;
  } else {
    document.getElementById('dt-usage-value').textContent = '-';
    document.getElementById('dt-threshold-value').textContent = '-';
    document.getElementById('dt-percentage-value').textContent = '0%';
    const progressFill = document.getElementById('dt-progress-fill');
    if (progressFill) {
      progressFill.style.width = '0%';
      progressFill.className = 'dt-progress-fill';
    }
    document.getElementById('dt-last-update').textContent = '暂无监控数据，请点击手动获取或开启监控';
  }

  // Update monitor status badge
  const statusBadge = document.getElementById('dt-monitor-status');
  if (data.running) {
    statusBadge.textContent = '运行中';
    statusBadge.className = 'dt-monitor-status-badge on';
  } else {
    statusBadge.textContent = config.enabled ? '已停止' : '未启用';
    statusBadge.className = 'dt-monitor-status-badge off';
  }

  // Update settings form
  document.getElementById('dt-enabled').checked = Boolean(config.enabled);
  document.getElementById('dt-interval').value = config.interval || 300;
  document.getElementById('dt-threshold-input').value = config.threshold || 9000;
  document.getElementById('dt-auto-stop').checked = Boolean(config.autoStop);
  document.getElementById('dt-stop-method').value = config.stopMethod || 'soft';
}

async function fetchDTManual() {
  if (!selectedAccount || selectedAccount.provider !== 'oci') return;
  const reqAccount = selectedAccount.account;
  const btn = document.getElementById('dt-fetch-btn');
  btn.disabled = true;
  btn.innerHTML = '<i class="bi bi-hourglass-split"></i> 查询中...';
  addLog(`正在查询 OCI 账号 ${reqAccount} 的数据传输用量...`, 'info');

  try {
    const result = await fetchJSON(`/api/oci/${encodeURIComponent(reqAccount)}/data-transfer`);
    if (selectedAccount && selectedAccount.account === reqAccount) {
      if (result.error) {
        addLog(`数据传输查询失败：${result.error}`, 'error');
      } else {
        addLog(`OCI ${reqAccount} 当月数据传输：${result.usageGB.toFixed(2)} GB / ${result.threshold.toFixed(0)} GB (${result.percentage.toFixed(1)}%)`, 'success');
      }
      loadDTMonitorStatus();
    }
  } catch (error) {
    if (selectedAccount && selectedAccount.account === reqAccount) {
      addLog(`数据传输查询失败：${error.message}`, 'error');
    }
  } finally {
    if (selectedAccount && selectedAccount.account === reqAccount) {
      btn.disabled = false;
      btn.innerHTML = '<i class="bi bi-arrow-clockwise"></i> 手动获取';
    }
  }
}

function toggleDTSettings() {
  const panel = document.getElementById('dt-settings-panel');
  panel.hidden = !panel.hidden;
}

async function saveDTConfig() {
  if (!selectedAccount || selectedAccount.provider !== 'oci') return;
  const msgEl = document.getElementById('dt-config-message');
  msgEl.textContent = '保存中...';
  msgEl.className = 'form-message';

  const autoStop = document.getElementById('dt-auto-stop').checked;
  if (autoStop && !confirm('⚠️ 你确定要启用"超阈值自动停机"功能吗？\n\n当数据传输用量超过阈值时，系统将自动停止该账号下所有正在运行的实例。')) {
    msgEl.textContent = '已取消。';
    msgEl.className = 'form-message';
    return;
  }

  const payload = {
    enabled: document.getElementById('dt-enabled').checked,
    interval: Math.max(60, Number(document.getElementById('dt-interval').value) || 300),
    threshold: Math.max(1, Number(document.getElementById('dt-threshold-input').value) || 9000),
    autoStop: autoStop,
    stopMethod: document.getElementById('dt-stop-method').value || 'soft'
  };

  try {
    await fetchJSON(`/api/oci/${encodeURIComponent(selectedAccount.account)}/data-transfer/config`, {
      method: 'POST',
      body: payload
    });
    msgEl.textContent = '监控配置已保存。';
    msgEl.className = 'form-message success';
    addLog(`OCI ${selectedAccount.account} 数据传输监控配置已保存。`, 'success');
    loadDTMonitorStatus();
  } catch (error) {
    msgEl.textContent = error.message;
    msgEl.className = 'form-message error';
  }
}

// Bind DT monitor events
document.addEventListener('DOMContentLoaded', () => {
  document.getElementById('dt-fetch-btn')?.addEventListener('click', fetchDTManual);
  document.getElementById('dt-settings-toggle')?.addEventListener('click', toggleDTSettings);
  document.getElementById('dt-save-config-btn')?.addEventListener('click', saveDTConfig);
});

// ==================== DNS Management Page ====================

function loadDNSPage() {
  loadCFAccounts();
  loadDNSBindingsList();
  loadDNSRaw();
}

function renderCFAccountRow(a = {}) {
  const name = a.name || '';
  const remark = a.remark || '';
  const apiToken = a.api_token || '';
  const zoneId = a.zone_id || '';
  
  return `
    <div class="dns-cf-row cf-row-item">
      <div class="dns-cf-fields">
        <div class="field compact">
          <span>账号名称</span>
          <input type="text" class="cf-name-input" value="${escapeAttr(name)}" placeholder="例：cf01" ${name ? 'disabled' : ''}>
        </div>
        <div class="field compact">
          <span>备注</span>
          <input type="text" class="cf-remark-input" value="${escapeAttr(remark)}" placeholder="备注/说明">
        </div>
        <div class="field compact">
          <span>API Token</span>
          <input type="text" class="cf-token-input" value="${escapeAttr(apiToken)}" placeholder="API Token">
        </div>
        <div class="field compact">
          <span>Zone ID</span>
          <input type="text" class="cf-zone-input" value="${escapeAttr(zoneId)}" placeholder="Zone ID">
        </div>
      </div>
      <button class="ghost-btn dns-remove-btn cf-row-delete-btn" type="button">删除</button>
    </div>
  `;
}

function addCFAccountRow() {
  const el = document.getElementById('cf-accounts-list');
  if (el.querySelector('.empty-state')) {
    el.innerHTML = '';
  }
  const div = document.createElement('div');
  div.innerHTML = renderCFAccountRow();
  el.appendChild(div.firstElementChild);
}

async function loadCFAccounts() {
  const el = document.getElementById('cf-accounts-list');
  try {
    const accounts = await fetchJSON('/api/dns/cloudflare');
    if (!Array.isArray(accounts) || accounts.length === 0) {
      el.innerHTML = '<div class="empty-state compact">暂无 Cloudflare 账号配置。</div>';
      return;
    }
    el.innerHTML = accounts.map(a => renderCFAccountRow(a)).join('');
  } catch (err) {
    el.innerHTML = `<div class="empty-state compact error">${escapeHtml(err.message)}</div>`;
  }
}

async function saveCFAccounts() {
  const el = document.getElementById('cf-accounts-list');
  const rows = el.querySelectorAll('.cf-row-item');
  const accounts = [];
  
  for (const row of rows) {
    const nameInput = row.querySelector('.cf-name-input');
    const name = nameInput.value.trim();
    if (!name) continue;
    
    const remark = row.querySelector('.cf-remark-input').value.trim();
    const apiToken = row.querySelector('.cf-token-input').value.trim();
    const zoneId = row.querySelector('.cf-zone-input').value.trim();
    
    accounts.push({ name, remark, api_token: apiToken, zone_id: zoneId });
  }
  
  const msgEl = document.getElementById('cf-message');
  msgEl.textContent = '保存中...';
  msgEl.className = 'form-message';
  
  try {
    await fetchJSON('/api/dns/cloudflare', {
      method: 'POST',
      body: { accounts }
    });
    msgEl.textContent = 'Cloudflare 配置保存成功。';
    msgEl.className = 'form-message success';
    addLog('Cloudflare 配置已更新并自动重载。', 'success');
    loadDNSPage();
  } catch (err) {
    msgEl.textContent = err.message;
    msgEl.className = 'form-message error';
  }
}

function maskDomain(domain) {
  if (!domain) return '';
  const parts = domain.split('.');
  if (parts.length <= 1) return domain;
  return parts[0] + '.' + parts.slice(1).map(() => '**').join('.');
}

function maskRawDNSConfig(rawText) {
  if (!rawText) return rawText;
  return rawText.replace(/^(domain\s*=\s*)([^\r\n]+)/gm, (match, prefix, domainVal) => {
    return prefix + maskDomain(domainVal.trim());
  });
}

async function loadDNSBindingsList() {
  const el = document.getElementById('dns-bindings-list');
  try {
    const bindings = await fetchJSON('/api/dns/bindings');
    if (!Array.isArray(bindings) || bindings.length === 0) {
      el.innerHTML = '<div class="empty-state compact">暂无 DNS 绑定。可在 VM 卡片中添加。</div>';
      return;
    }
    el.innerHTML = bindings.map(b => `
      <div class="dns-binding-card">
        <div class="dns-binding-info">
          <strong>${escapeHtml(b.name)}</strong>
          <span>${escapeHtml(b.provider)}/${escapeHtml(b.account)} → ${escapeHtml(maskDomain(b.domain))}</span>
          <span class="dns-binding-detail">CF: ${escapeHtml(b.cloudflare)} | VM: ${escapeHtml(b.vm)} | ${escapeHtml(b.type)} | TTL=${b.ttl} | Proxied=${b.proxied}</span>
        </div>
        <button class="ghost-btn dns-delete-binding-btn" type="button" data-name="${escapeAttr(b.name)}">删除</button>
      </div>
    `).join('');
  } catch (err) {
    el.innerHTML = `<div class="empty-state compact error">${escapeHtml(err.message)}</div>`;
  }
}

async function deleteDNSBindingByName(name) {
  if (!confirm(`确认删除 DNS 绑定「${name}」？`)) return;
  try {
    await fetchJSON('/api/dns/delete-binding', { method: 'POST', body: { name } });
    addLog(`已删除 DNS 绑定：${name}`, 'success');
    loadDNSBindingsList();
    loadDNSRaw();
    if (selectedAccount) fetchVMs();
  } catch (err) {
    addLog(`删除失败：${err.message}`, 'error');
  }
}

async function loadDNSRaw() {
  const el = document.getElementById('dns-raw-content');
  try {
    const data = await fetchJSON('/api/dns/raw');
    el.textContent = maskRawDNSConfig(data.content) || '（空）';
  } catch (err) {
    el.textContent = `加载失败：${err.message}`;
  }
}

async function reloadDNSConfig() {
  try {
    await fetchJSON('/api/config/reload', { method: 'POST' });
    addLog('DNS 配置已重载。', 'success');
    loadDNSPage();
  } catch (err) {
    addLog(`重载失败：${err.message}`, 'error');
  }
}

// Bind DNS page events
document.addEventListener('DOMContentLoaded', () => {
  document.getElementById('dns-reload-btn')?.addEventListener('click', reloadDNSConfig);
  document.getElementById('add-cf-account-btn')?.addEventListener('click', addCFAccountRow);
  document.getElementById('save-cf-accounts-btn')?.addEventListener('click', saveCFAccounts);
  document.getElementById('dns-raw-refresh-btn')?.addEventListener('click', loadDNSRaw);
  
  // Delete binding delegation
  document.getElementById('dns-bindings-list')?.addEventListener('click', e => {
    const btn = e.target.closest('.dns-delete-binding-btn');
    if (btn) deleteDNSBindingByName(btn.dataset.name);
  });
  
  // Delete CF account row delegation
  document.getElementById('cf-accounts-list')?.addEventListener('click', e => {
    const btn = e.target.closest('.cf-row-delete-btn');
    if (btn) {
      const row = btn.closest('.cf-row-item');
      if (row) row.remove();
      const el = document.getElementById('cf-accounts-list');
      if (el.children.length === 0) {
        el.innerHTML = '<div class="empty-state compact">暂无 Cloudflare 账号配置。</div>';
      }
    }
  });
});

let dnsModalVM = null;
let dnsModalCFAccounts = [];

function openDNSBindingModal(vm) {
  dnsModalVM = vm;
  const modal = document.getElementById('dns-binding-modal');
  document.getElementById('dns-modal-title').textContent = `DNS 绑定 - ${vm.provider}/${vm.accountId}/${vm.id}`;
  document.getElementById('dns-modal-body').innerHTML = '<div class="empty-state compact">加载中...</div>';
  document.getElementById('dns-modal-message').textContent = '';
  modal.hidden = false;
  loadVMDNSBindings(vm);
}

function closeDNSModal() {
  document.getElementById('dns-binding-modal').hidden = true;
  dnsModalVM = null;
}

async function loadVMDNSBindings(vm) {
  try {
    const data = await fetchJSON(`/api/vm/${encodeURIComponent(vm.provider)}/${encodeURIComponent(vm.accountId)}/${encodeURIComponent(vm.id)}/dns`);
    dnsModalCFAccounts = Array.isArray(data.cloudflare_accounts) ? data.cloudflare_accounts : [];
    const bindings = Array.isArray(data.bindings) ? data.bindings : [];
    renderDNSModalBindings(bindings);
  } catch (err) {
    document.getElementById('dns-modal-body').innerHTML = `<div class="empty-state compact error">${escapeHtml(err.message)}</div>`;
  }
}

function cfSelectHtml(selected) {
  return dnsModalCFAccounts.map(cf => `<option value="${escapeAttr(cf.name)}" ${cf.name === selected ? 'selected' : ''}>${escapeHtml(cf.name)}</option>`).join('');
}

function renderDNSModalBindings(bindings) {
  const body = document.getElementById('dns-modal-body');
  if (bindings.length === 0) {
    body.innerHTML = '<div class="empty-state compact">暂无 DNS 绑定，点击下方“添加绑定”创建。</div>';
    return;
  }
  body.innerHTML = bindings.map((b, i) => dnsBindingRowHtml(b, i)).join('');
}

function dnsBindingRowHtml(b, i) {
  return `<div class="dns-binding-row" data-index="${i}">
  <div class="dns-binding-fields">
    <label class="field compact"><span>绑定名称</span><input type="text" class="dns-f-name" value="${escapeAttr(b.name || '')}"></label>
    <label class="field compact"><span>Cloudflare</span><select class="dns-f-cf">${cfSelectHtml(b.cloudflare)}</select></label>
    <label class="field compact"><span>域名</span><input type="text" class="dns-f-domain" value="${escapeAttr(b.domain || '')}" placeholder="sub.example.com"></label>
    <label class="field compact"><span>类型</span><select class="dns-f-type"><option value="A" ${b.type==='A'?'selected':''}>A</option><option value="AAAA" ${b.type==='AAAA'?'selected':''}>AAAA</option><option value="CNAME" ${b.type==='CNAME'?'selected':''}>CNAME</option></select></label>
    <label class="field compact"><span>TTL</span><input type="number" class="dns-f-ttl" value="${b.ttl||1}" min="1"></label>
    <label class="switch-row compact"><input type="checkbox" class="dns-f-proxied" ${b.proxied?'checked':''}><span>Proxied</span></label>
  </div>
  <button class="ghost-btn dns-remove-btn" type="button" data-index="${i}">删除</button>
</div>`;
}

function addDNSBindingRow() {
  if (!dnsModalVM) return;
  const body = document.getElementById('dns-modal-body');
  if (body.querySelector('.empty-state')) body.innerHTML = '';
  const vm = dnsModalVM;
  const idx = body.querySelectorAll('.dns-binding-row').length;
  const name = `${vm.provider}-${vm.accountId}-${vm.id}-${idx}`;
  const cf = dnsModalCFAccounts.length > 0 ? dnsModalCFAccounts[0].name : '';
  body.insertAdjacentHTML('beforeend', dnsBindingRowHtml({ name, cloudflare: cf, domain: '', type: 'A', ttl: 1, proxied: false }, idx));
}

function removeDNSBindingRow(index) {
  const body = document.getElementById('dns-modal-body');
  const row = body.querySelector(`.dns-binding-row[data-index="${index}"]`);
  if (row) row.remove();
  if (!body.querySelector('.dns-binding-row')) {
    body.innerHTML = '<div class="empty-state compact">暂无 DNS 绑定</div>';
  }
}

async function saveDNSModal() {
  if (!dnsModalVM) return;
  const vm = dnsModalVM;
  const rows = document.getElementById('dns-modal-body').querySelectorAll('.dns-binding-row');
  const msgEl = document.getElementById('dns-modal-message');
  const bindings = [];
  for (const row of rows) {
    bindings.push({
      name: row.querySelector('.dns-f-name')?.value?.trim() || '',
      cloudflare: row.querySelector('.dns-f-cf')?.value || '',
      domain: row.querySelector('.dns-f-domain')?.value?.trim() || '',
      type: row.querySelector('.dns-f-type')?.value || 'A',
      ttl: Number(row.querySelector('.dns-f-ttl')?.value) || 1,
      proxied: row.querySelector('.dns-f-proxied')?.checked || false
    });
  }
  msgEl.textContent = '保存中...';
  msgEl.className = 'form-message';
  try {
    await fetchJSON(`/api/vm/${encodeURIComponent(vm.provider)}/${encodeURIComponent(vm.accountId)}/${encodeURIComponent(vm.id)}/dns`, { method: 'POST', body: { bindings } });
    msgEl.textContent = '已保存并重载生效。';
    msgEl.className = 'form-message success';
    addLog(`DNS 绑定已保存：${vm.provider}/${vm.accountId}/${vm.id}`, 'success');
    if (selectedAccount) fetchVMs();
  } catch (err) {
    msgEl.textContent = err.message;
    msgEl.className = 'form-message error';
  }
}

document.addEventListener('DOMContentLoaded', () => {
  document.getElementById('dns-modal-close')?.addEventListener('click', closeDNSModal);
  document.getElementById('dns-modal-add')?.addEventListener('click', addDNSBindingRow);
  document.getElementById('dns-modal-save')?.addEventListener('click', saveDNSModal);
  document.getElementById('dns-binding-modal')?.addEventListener('click', e => {
    if (e.target.id === 'dns-binding-modal') closeDNSModal();
  });
  document.getElementById('dns-modal-body')?.addEventListener('click', e => {
    const btn = e.target.closest('.dns-remove-btn');
    if (btn) removeDNSBindingRow(btn.dataset.index);
  });
});

// ==================== OCI Instance Edit ====================

let ociEditModalVM = null;
let ociEditOptions = null;
let ociEditLoadSeq = 0;

const OCI_SHAPE_FAMILIES = [
  { key: 'amd', label: 'AMD', description: '弹性 OCPU 数。当代 AMD 处理器。' },
  { key: 'intel', label: 'Intel', description: '弹性 OCPU 数。当代 Intel 处理器。' },
  { key: 'ampere', label: 'Ampere', description: '基于 ARM 的处理器。' },
  { key: 'special', label: '专用和上一代', description: '密集 I/O、GPU、HPC、Generic 以及旧规格。' }
];

function openOCIEditModal(vm) {
  ociEditModalVM = vm;
  ociEditOptions = null;
  document.getElementById('oci-edit-title').textContent = `编辑 OCI 实例 - ${vm.accountId}/${vm.name}`;
  document.getElementById('oci-edit-body').innerHTML = '<div class="empty-state compact">加载中...</div>';
  document.getElementById('oci-edit-message').textContent = '';
  document.getElementById('oci-edit-message').className = 'form-message';
  document.getElementById('oci-edit-save').disabled = true;
  document.getElementById('oci-edit-modal').hidden = false;
  loadOCIEditOptions(vm);
}

function closeOCIEditModal() {
  document.getElementById('oci-edit-modal').hidden = true;
  ociEditModalVM = null;
  ociEditOptions = null;
}

async function loadOCIEditOptions(vm, shape = '', preserve = {}) {
  const seq = ++ociEditLoadSeq;
  const msgEl = document.getElementById('oci-edit-message');
  if (shape) {
    msgEl.textContent = '正在读取该规格的可用上限...';
    msgEl.className = 'form-message';
  }

  try {
    const query = shape ? `?shape=${encodeURIComponent(shape)}` : '';
    const data = await fetchJSON(`/api/vm/${encodeURIComponent(vm.provider)}/${encodeURIComponent(vm.accountId)}/${encodeURIComponent(vm.id)}/edit-options${query}`);
    if (seq !== ociEditLoadSeq || !ociEditModalVM) return;
    ociEditOptions = data;
    renderOCIEditForm(data, preserve);
    document.getElementById('oci-edit-save').disabled = false;
  } catch (err) {
    if (seq !== ociEditLoadSeq) return;
    document.getElementById('oci-edit-body').innerHTML = `<div class="empty-state compact error">${escapeHtml(err.message)}</div>`;
    document.getElementById('oci-edit-save').disabled = true;
  }
}

function currentOCIEditFormValues() {
  return {
    displayName: document.getElementById('oci-edit-display-name')?.value?.trim() || '',
    allowDowntime: document.getElementById('oci-edit-allow-downtime')?.checked ?? true
  };
}

function renderOCIEditForm(data, preserve = {}) {
  const instance = data.instance || {};
  const shape = data.selectedShape || {};
  const sameShape = shape.name === instance.shape;
  const ocpus = preserve.ocpus ?? (sameShape ? instance.ocpus : ociShapeDefaultOcpus(shape, data.limits?.ocpu));
  const memory = preserve.memoryInGBs ?? (sameShape ? instance.memoryInGBs : ociShapeDefaultMemory(shape, ocpus, data.limits?.memory));
  const displayName = preserve.displayName ?? instance.displayName ?? instance.id ?? '';
  const allowDowntime = preserve.allowDowntime ?? true;

  document.getElementById('oci-edit-body').innerHTML = `
    <div class="oci-edit-summary">
      <div><span>当前规格</span><strong>${escapeHtml(instance.shape || '-')}</strong></div>
      <div><span>当前 OCPU</span><strong>${escapeHtml(formatOCIAmount(instance.ocpus || 0))}</strong></div>
      <div><span>当前内存</span><strong>${escapeHtml(formatOCIAmount(instance.memoryInGBs || 0))} GB</strong></div>
      <div><span>可用区</span><strong>${escapeHtml(instance.availabilityDomain || '-')}</strong></div>
    </div>

    <label class="field">
      <span>实例名称</span>
      <input id="oci-edit-display-name" type="text" value="${escapeAttr(displayName)}">
    </label>

    <div class="oci-edit-section">
      <div class="oci-edit-section-title">配置系列</div>
      <div class="oci-family-grid">
        ${ociFamilyCardsHTML(data.shapes || [], shape.family)}
      </div>
    </div>

    <label class="field">
      <span>配置名称</span>
      <select id="oci-edit-shape">
        ${ociShapeOptionsHTML(data.shapes || [], shape.name)}
      </select>
    </label>

    <div class="oci-selected-shape">
      <div>
        <strong>${escapeHtml(shape.name || '-')}</strong>
        <span>${escapeHtml(shape.processorDescription || (shape.isFlexible ? 'Flex shape' : 'Fixed shape'))}</span>
      </div>
      <div class="oci-shape-badges">
        ${shape.isFlexible ? '<span>Flex</span>' : '<span>固定规格</span>'}
        ${shape.maxVnicAttachments ? `<span>最大 VNIC ${escapeHtml(shape.maxVnicAttachments)}</span>` : ''}
      </div>
    </div>

    <div class="oci-edit-grid">
      <label class="field">
        <span>OCPU 数</span>
        <input id="oci-edit-ocpus" type="number" min="0" step="0.25" value="${escapeAttr(ociNumberInputValue(ocpus))}" ${shape.isFlexible ? '' : 'disabled'}>
        <small id="oci-edit-ocpu-range" class="field-hint"></small>
      </label>
      <label class="field">
        <span>内存（GB）</span>
        <input id="oci-edit-memory" type="number" min="0" step="1" value="${escapeAttr(ociNumberInputValue(memory))}" ${shape.isFlexible ? '' : 'disabled'}>
        <small id="oci-edit-memory-range" class="field-hint"></small>
      </label>
    </div>

    <label class="switch-row oci-downtime-row">
      <input id="oci-edit-allow-downtime" type="checkbox" ${allowDowntime ? 'checked' : ''}>
      <span>允许停机完成规格变更</span>
    </label>

    <div id="oci-edit-limit-note" class="oci-limit-note"></div>
  `;

  updateOCIEditRangeHints();
  renderOCIEditWarnings(data.warnings || []);
}

function ociFamilyCardsHTML(shapes, selectedFamily) {
  return OCI_SHAPE_FAMILIES.map(family => {
    const count = shapes.filter(shape => shape.family === family.key).length;
    const disabled = count === 0;
    return `
      <button class="oci-family-card ${selectedFamily === family.key ? 'active' : ''}" data-family="${escapeAttr(family.key)}" type="button" ${disabled ? 'disabled' : ''}>
        <strong>${escapeHtml(family.label)}</strong>
        <span>${escapeHtml(family.description)}</span>
      </button>
    `;
  }).join('');
}

function ociShapeOptionsHTML(shapes, selectedName) {
  return OCI_SHAPE_FAMILIES.map(family => {
    const familyShapes = shapes.filter(shape => shape.family === family.key);
    if (familyShapes.length === 0) return '';
    return `
      <optgroup label="${escapeAttr(family.label)}">
        ${familyShapes.map(shape => `
          <option value="${escapeAttr(shape.name)}" ${shape.name === selectedName ? 'selected' : ''}>
            ${escapeHtml(shape.name)}${shape.isFlexible ? ' · Flex' : ''}
          </option>
        `).join('')}
      </optgroup>
    `;
  }).join('');
}

function updateOCIEditRangeHints() {
  if (!ociEditOptions?.selectedShape) return;
  const shape = ociEditOptions.selectedShape;
  const ocpuInput = document.getElementById('oci-edit-ocpus');
  const memoryInput = document.getElementById('oci-edit-memory');
  const ocpuRangeEl = document.getElementById('oci-edit-ocpu-range');
  const memoryRangeEl = document.getElementById('oci-edit-memory-range');
  const noteEl = document.getElementById('oci-edit-limit-note');
  if (!ocpuInput || !memoryInput || !ocpuRangeEl || !memoryRangeEl) return;

  const ranges = ociEditComputedRanges();
  ocpuInput.min = ranges.ocpuMin;
  ocpuInput.max = ranges.ocpuMax;
  memoryInput.min = ranges.memoryMin;
  memoryInput.max = ranges.memoryMax;

  ocpuRangeEl.textContent = ociRangeText('OCPU', ranges.ocpuMin, ranges.ocpuMax, ociEditOptions.limits?.ocpu);
  memoryRangeEl.textContent = ociRangeText('内存', ranges.memoryMin, ranges.memoryMax, ociEditOptions.limits?.memory, 'GB');

  const notes = [];
  if (!shape.isFlexible) {
    notes.push('当前选择的是固定规格，OCPU 和内存会随规格自动确定。');
  }
  notes.push('当前仅显示规格允许范围；账号配额和实时容量由 OCI 在保存时最终校验。');
  noteEl.textContent = notes.join(' ');
}

function ociEditComputedRanges() {
  const shape = ociEditOptions.selectedShape || {};
  const limits = ociEditOptions.limits || {};
  const ocpuLimit = limits.ocpu || {};
  const memoryLimit = limits.memory || {};

  const ocpuMin = numberOrFallback(ocpuLimit.shapeMin, shape.ocpuOptions?.min, shape.ocpus, 0);
  const ocpuMax = numberOrFallback(ocpuLimit.effectiveMax, ocpuLimit.shapeMax, shape.ocpuOptions?.max, shape.ocpus, ocpuMin);
  const selectedOcpus = Number(document.getElementById('oci-edit-ocpus')?.value || ocpuMin) || ocpuMin;

  let memoryMin = numberOrFallback(memoryLimit.shapeMin, shape.memoryOptions?.minInGBs, shape.memoryInGBs, 0);
  let memoryMax = numberOrFallback(memoryLimit.effectiveMax, memoryLimit.shapeMax, shape.memoryOptions?.maxInGBs, shape.memoryInGBs, memoryMin);
  if (shape.memoryOptions?.minPerOcpuInGBs) {
    memoryMin = Math.max(memoryMin, selectedOcpus * Number(shape.memoryOptions.minPerOcpuInGBs));
  }
  if (shape.memoryOptions?.maxPerOcpuInGBs) {
    memoryMax = Math.min(memoryMax, selectedOcpus * Number(shape.memoryOptions.maxPerOcpuInGBs));
  }

  return { ocpuMin, ocpuMax, memoryMin, memoryMax };
}

function ociRangeText(label, min, max, limit = {}, unit = '') {
  const suffix = unit ? ` ${unit}` : '';
  return `${label} 应介于 ${formatOCIAmount(min)} 和 ${formatOCIAmount(max)}${suffix} 之间。规格范围 ${formatOCIAmount(min)} - ${formatOCIAmount(limit.shapeMax || max)}${suffix}`;
}

function renderOCIEditWarnings(warnings) {
  const msgEl = document.getElementById('oci-edit-message');
  if (!warnings.length) {
    msgEl.textContent = '';
    msgEl.className = 'form-message';
    return;
  }
  msgEl.textContent = warnings.join('；');
  msgEl.className = 'form-message';
}

function ociShapeDefaultOcpus(shape = {}, limit = {}) {
  return numberOrFallback(shape.memoryOptions?.defaultOcpus, shape.ocpus, limit.shapeMin, shape.ocpuOptions?.min, 0);
}

function ociShapeDefaultMemory(shape = {}, ocpus = 0, limit = {}) {
  const direct = numberOrFallback(shape.memoryOptions?.defaultMemoryInGBs, shape.memoryInGBs, 0);
  if (direct > 0) return direct;
  if (shape.memoryOptions?.defaultPerOcpuInGBs && ocpus > 0) {
    return Number(shape.memoryOptions.defaultPerOcpuInGBs) * ocpus;
  }
  return numberOrFallback(limit.shapeMin, shape.memoryOptions?.minInGBs, 0);
}

function ociNumberInputValue(value) {
  const n = Number(value);
  if (!Number.isFinite(n) || n <= 0) return '';
  return formatOCIAmount(n);
}

function numberOrFallback(...values) {
  for (const value of values) {
    const n = Number(value);
    if (Number.isFinite(n) && n > 0) return n;
  }
  return 0;
}

function formatOCIAmount(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return '0';
  if (Math.abs(n - Math.round(n)) < 0.000001) return String(Math.round(n));
  return n.toFixed(2).replace(/\.?0+$/, '');
}

async function saveOCIEditModal() {
  if (!ociEditModalVM || !ociEditOptions?.selectedShape) return;
  const msgEl = document.getElementById('oci-edit-message');
  const saveBtn = document.getElementById('oci-edit-save');
  const shape = ociEditOptions.selectedShape;
  const ranges = ociEditComputedRanges();
  const displayName = document.getElementById('oci-edit-display-name')?.value?.trim() || '';
  const ocpus = Number(document.getElementById('oci-edit-ocpus')?.value || 0);
  const memoryInGBs = Number(document.getElementById('oci-edit-memory')?.value || 0);
  const allowDowntime = document.getElementById('oci-edit-allow-downtime')?.checked === true;

  if (!displayName) {
    msgEl.textContent = '实例名称不能为空。';
    msgEl.className = 'form-message error';
    return;
  }
  if (shape.isFlexible) {
    if (ocpus < ranges.ocpuMin || ocpus > ranges.ocpuMax) {
      msgEl.textContent = `OCPU 数应介于 ${formatOCIAmount(ranges.ocpuMin)} 和 ${formatOCIAmount(ranges.ocpuMax)} 之间。`;
      msgEl.className = 'form-message error';
      return;
    }
    if (memoryInGBs < ranges.memoryMin || memoryInGBs > ranges.memoryMax) {
      msgEl.textContent = `内存应介于 ${formatOCIAmount(ranges.memoryMin)} GB 和 ${formatOCIAmount(ranges.memoryMax)} GB 之间。`;
      msgEl.className = 'form-message error';
      return;
    }
  }
  if (allowDowntime && ociEditModalVM.status === 'VM running' && !confirm('调整 OCI 规格可能导致实例短暂重启，确认提交？')) {
    return;
  }

  msgEl.textContent = '正在提交 OCI 实例编辑请求...';
  msgEl.className = 'form-message';
  saveBtn.disabled = true;

  try {
    const vm = ociEditModalVM;
    const payload = {
      displayName,
      shape: shape.name,
      ocpus,
      memoryInGBs,
      baselineOcpuUtilization: ociEditOptions.instance?.baselineOcpuUtilization || '',
      allowDowntime
    };
    const data = await fetchJSON(`/api/vm/${encodeURIComponent(vm.provider)}/${encodeURIComponent(vm.accountId)}/${encodeURIComponent(vm.id)}/edit`, {
      method: 'POST',
      body: payload
    });
    addLog(data.message || `OCI 实例 ${vm.name} 编辑请求已提交。`, 'success');
    closeOCIEditModal();
    setTimeout(() => refreshVM(vm), 5000);
  } catch (err) {
    msgEl.textContent = err.message;
    msgEl.className = 'form-message error';
  } finally {
    saveBtn.disabled = false;
  }
}

document.addEventListener('DOMContentLoaded', () => {
  document.getElementById('oci-edit-close')?.addEventListener('click', closeOCIEditModal);
  document.getElementById('oci-edit-cancel')?.addEventListener('click', closeOCIEditModal);
  document.getElementById('oci-edit-save')?.addEventListener('click', saveOCIEditModal);
  document.getElementById('oci-edit-modal')?.addEventListener('click', event => {
    if (event.target.id === 'oci-edit-modal') closeOCIEditModal();
  });
  document.getElementById('oci-edit-body')?.addEventListener('click', event => {
    const familyBtn = event.target.closest('.oci-family-card[data-family]');
    if (!familyBtn || familyBtn.disabled || !ociEditModalVM || !ociEditOptions) return;
    const family = familyBtn.dataset.family;
    const nextShape = (ociEditOptions.shapes || []).find(shape => shape.family === family);
    if (!nextShape || nextShape.name === ociEditOptions.selectedShape?.name) return;
    loadOCIEditOptions(ociEditModalVM, nextShape.name, currentOCIEditFormValues());
  });
  document.getElementById('oci-edit-body')?.addEventListener('change', event => {
    if (event.target.id === 'oci-edit-shape' && ociEditModalVM) {
      loadOCIEditOptions(ociEditModalVM, event.target.value, currentOCIEditFormValues());
    }
  });
  document.getElementById('oci-edit-body')?.addEventListener('input', event => {
    if (event.target.id === 'oci-edit-ocpus') {
      updateOCIEditRangeHints();
    }
  });
});

let securityListModalVM = null;
let securityListModalLists = [];
let securityListModalNSGs = [];
let securityListModalDirection = 'ingress';
let securityListModalResourceType = 'security-list';

const SECURITY_PROTOCOL_OPTIONS = [
  { value: 'all', label: '所有协议' },
  { value: '1', label: 'ICMP' },
  { value: '6', label: 'TCP' },
  { value: '17', label: 'UDP' },
  { value: '6', label: 'SSH (TCP/22)', minPort: 22, maxPort: 22 },
  { value: '6', label: 'HTTP (TCP/80)', minPort: 80, maxPort: 80 },
  { value: '6', label: 'HTTPS (TCP/443)', minPort: 443, maxPort: 443 },
  { value: '6', label: 'RDP (TCP/3389)', minPort: 3389, maxPort: 3389 },
  { value: '50', label: 'ESP (50)' },
  { value: '51', label: 'AH (51)' },
  { value: '58', label: 'IPv6 ICMP (58)' },
  { value: '47', label: 'GRE (47)' },
  { value: '132', label: 'SCTP (132)' },
  { value: '4', label: 'IPv4 (4)' },
  { value: '41', label: 'IPv6 (41)' }
];

const ICMP_TYPE_OPTIONS = [
  { value: '', label: '全部' },
  { value: '0', label: '0 - Echo Reply' },
  { value: '3', label: '3 - Destination Unreachable' },
  { value: '4', label: '4 - Source Quench' },
  { value: '5', label: '5 - Redirect' },
  { value: '8', label: '8 - Echo Request (Ping)' },
  { value: '9', label: '9 - Router Advertisement' },
  { value: '10', label: '10 - Router Solicitation' },
  { value: '11', label: '11 - Time Exceeded' },
  { value: '12', label: '12 - Parameter Problem' },
  { value: '13', label: '13 - Timestamp' },
  { value: '14', label: '14 - Timestamp Reply' }
];

const ICMP_CODE_OPTIONS = {
  '': [{ value: '', label: '全部' }],
  '0': [{ value: '', label: '全部' }, { value: '0', label: '0 - Echo Reply' }],
  '3': [
    { value: '', label: '全部' },
    { value: '0', label: '0 - Network Unreachable' },
    { value: '1', label: '1 - Host Unreachable' },
    { value: '2', label: '2 - Protocol Unreachable' },
    { value: '3', label: '3 - Port Unreachable' },
    { value: '4', label: '4 - Fragmentation Needed' },
    { value: '13', label: '13 - Communication Administratively Prohibited' }
  ],
  '5': [
    { value: '', label: '全部' },
    { value: '0', label: '0 - Redirect Datagram for Network' },
    { value: '1', label: '1 - Redirect Datagram for Host' }
  ],
  '8': [{ value: '', label: '全部' }, { value: '0', label: '0 - Echo Request' }],
  '11': [
    { value: '', label: '全部' },
    { value: '0', label: '0 - TTL Exceeded' },
    { value: '1', label: '1 - Fragment Reassembly Time Exceeded' }
  ],
  '12': [
    { value: '', label: '全部' },
    { value: '0', label: '0 - Pointer Indicates Error' },
    { value: '1', label: '1 - Missing Required Option' },
    { value: '2', label: '2 - Bad Length' }
  ]
};

function openSecurityListModal(vm) {
  securityListModalVM = vm;
  securityListModalResourceType = 'security-list';
  securityListModalDirection = 'ingress';
  const modal = document.getElementById('security-list-modal');
  document.getElementById('sg-modal-title').textContent = `OCI 安全规则 - ${vm.accountId}/${vm.name}`;
  document.getElementById('sg-modal-list').innerHTML = '';
  document.getElementById('sg-modal-body').innerHTML = '<div class="empty-state compact">加载中...</div>';
  document.getElementById('sg-modal-message').textContent = '';
  document.getElementById('sg-new-nsg-name').value = '';
  updateSecurityResourceTabs();
  updateSecurityRuleDirectionTabs();
  modal.hidden = false;
  loadSecurityLists(vm);
}

function closeSecurityListModal() {
  document.getElementById('security-list-modal').hidden = true;
  securityListModalVM = null;
  securityListModalLists = [];
  securityListModalNSGs = [];
}

async function loadSecurityLists(vm, selectedListId = '') {
  const listSelect = document.getElementById('sg-modal-list');
  const body = document.getElementById('sg-modal-body');
  const msgEl = document.getElementById('sg-modal-message');
  msgEl.textContent = '';
  document.getElementById('sg-modal-resource-label').textContent = securityListModalResourceType === 'network-security-group'
    ? '网络安全组'
    : '安全列表';
  document.getElementById('sg-nsg-create-row').hidden = securityListModalResourceType !== 'network-security-group';

  try {
    const path = securityListModalResourceType === 'network-security-group'
      ? `/api/vm/${encodeURIComponent(vm.provider)}/${encodeURIComponent(vm.accountId)}/${encodeURIComponent(vm.id)}/network-security-groups`
      : `/api/vm/${encodeURIComponent(vm.provider)}/${encodeURIComponent(vm.accountId)}/${encodeURIComponent(vm.id)}/security-lists`;
    const data = await fetchJSON(path);
    if (securityListModalResourceType === 'network-security-group') {
      securityListModalNSGs = Array.isArray(data.networkSecurityGroups) ? data.networkSecurityGroups : [];
    } else {
      securityListModalLists = Array.isArray(data.securityLists) ? data.securityLists : [];
    }

    const resources = currentSecurityResources();
    if (resources.length === 0) {
      listSelect.innerHTML = securityListModalResourceType === 'network-security-group'
        ? '<option value="">未关联网络安全组</option>'
        : '<option value="">未关联安全列表</option>';
      body.innerHTML = securityListModalResourceType === 'network-security-group'
        ? '<div class="empty-state compact">主 VNIC 未关联网络安全组，可在上方创建并关联。</div>'
        : '<div class="empty-state compact">主 VNIC 所在子网未关联 OCI 安全列表。</div>';
      return;
    }

    const nextSelected = selectedListId || resources[0].id;
    listSelect.innerHTML = resources.map(list => `
      <option value="${escapeAttr(list.id)}" ${list.id === nextSelected ? 'selected' : ''}>${escapeHtml(list.name || list.id)}</option>
    `).join('');
    renderSecurityListRules(nextSelected);
  } catch (err) {
    body.innerHTML = `<div class="empty-state compact error">${escapeHtml(err.message)}</div>`;
  }
}

function selectedSecurityList() {
  const selectedID = document.getElementById('sg-modal-list')?.value || '';
  return currentSecurityResources().find(list => list.id === selectedID) || null;
}

function currentSecurityResources() {
  return securityListModalResourceType === 'network-security-group'
    ? securityListModalNSGs
    : securityListModalLists;
}

function renderSecurityListRules(listId) {
  const body = document.getElementById('sg-modal-body');
  const list = currentSecurityResources().find(item => item.id === listId);
  if (!list) {
    body.innerHTML = '<div class="empty-state compact">请选择安全列表。</div>';
    return;
  }

  const rules = securityListModalDirection === 'egress'
    ? (Array.isArray(list.egressRules) ? list.egressRules : [])
    : (Array.isArray(list.ingressRules) ? list.ingressRules : []);
  if (rules.length === 0) {
    body.innerHTML = `<div class="empty-state compact">暂无${securityListModalDirection === 'egress' ? '出站' : '入站'}规则，点击下方“添加安全规则”创建。</div>`;
    return;
  }

  body.innerHTML = rules.map((rule, index) => securityListRuleRowHtml(rule, index)).join('');
  updateSecurityListProtocolControls();
}

function securityListRuleRowHtml(rule = {}, index = 0) {
  const protocol = normalizeSecurityListProtocol(rule.protocol || '6');
  const minPort = rule.minPort ?? '';
  const maxPort = rule.maxPort ?? '';
  const icmpType = rule.icmpType ?? '';
  const icmpCode = rule.icmpCode ?? '';
  const endpoint = securityListModalDirection === 'egress'
    ? (rule.destination || '0.0.0.0/0')
    : (rule.source || '0.0.0.0/0');
  const endpointType = securityListModalDirection === 'egress'
    ? (rule.destinationType || 'CIDR_BLOCK')
    : (rule.sourceType || 'CIDR_BLOCK');
  const description = rule.description || '';
  const rowLabel = `规则 ${index + 1}`;
  const endpointLabel = securityListModalDirection === 'egress' ? '目标 CIDR' : '来源 CIDR';
  const endpointTypeLabel = securityListModalDirection === 'egress' ? '目标类型' : '来源类型';
  const protocolOption = SECURITY_PROTOCOL_OPTIONS.find(option =>
    option.value === protocol && (option.minPort ?? '') === minPort && (option.maxPort ?? '') === maxPort
  );
  const protocolSelectValue = protocolOption ? securityProtocolOptionValue(protocolOption) : securityProtocolOptionValue({value: protocol});

  return `<div class="sg-rule-row" data-rule-id="${escapeAttr(rule.id || '')}">
    <div class="sg-rule-meta">
      <strong>${escapeHtml(rowLabel)}</strong>
      <span>${rule.id ? escapeHtml(rule.id) : (securityListModalDirection === 'egress' ? '出站' : '入站') + '安全规则'}</span>
    </div>
    <div class="sg-rule-fields">
      <label class="field compact">
        <span>协议</span>
        <select class="sg-f-protocol">${securityProtocolOptionsHtml(protocolSelectValue)}</select>
      </label>
      <label class="field compact">
        <span>${endpointLabel}</span>
        <input type="text" class="sg-f-endpoint" value="${escapeAttr(endpoint)}" placeholder="0.0.0.0/0">
      </label>
      <label class="field compact">
        <span>端口起</span>
        <input type="number" class="sg-f-min-port" min="1" max="65535" value="${escapeAttr(minPort)}" placeholder="全部">
      </label>
      <label class="field compact">
        <span>端口止</span>
        <input type="number" class="sg-f-max-port" min="1" max="65535" value="${escapeAttr(maxPort)}" placeholder="同起始">
      </label>
      <label class="field compact">
        <span>ICMP 类型</span>
        <select class="sg-f-icmp-type">${icmpTypeOptionsHtml(icmpType)}</select>
      </label>
      <label class="field compact">
        <span>ICMP 代码</span>
        <select class="sg-f-icmp-code">${icmpCodeOptionsHtml(icmpType, icmpCode)}</select>
      </label>
      <label class="field compact">
        <span>${endpointTypeLabel}</span>
        <select class="sg-f-endpoint-type">
          <option value="CIDR_BLOCK" ${endpointType === 'CIDR_BLOCK' ? 'selected' : ''}>CIDR</option>
          <option value="SERVICE_CIDR_BLOCK" ${endpointType === 'SERVICE_CIDR_BLOCK' ? 'selected' : ''}>Service CIDR</option>
          <option value="NETWORK_SECURITY_GROUP" ${endpointType === 'NETWORK_SECURITY_GROUP' ? 'selected' : ''}>Network Security Group</option>
        </select>
      </label>
      <label class="switch-row compact">
        <input type="checkbox" class="sg-f-stateless" ${rule.isStateless ? 'checked' : ''}>
        <span>无状态</span>
      </label>
      <label class="field compact sg-description-field">
        <span>描述</span>
        <input type="text" class="sg-f-description" value="${escapeAttr(description)}" placeholder="可选">
      </label>
    </div>
    <div class="sg-rule-footer">
      <div class="sg-allow-summary">
        <span>允许</span>
        <strong>${escapeHtml(securityRuleAllowText({ protocol, minPort, maxPort, icmpType, icmpCode }))}</strong>
      </div>
      <button class="ghost-btn dns-remove-btn sg-rule-delete-btn" type="button">删除</button>
    </div>
  </div>`;
}

function normalizeSecurityListProtocol(protocol) {
  const value = String(protocol || '').toLowerCase();
  if (value === 'tcp') return '6';
  if (value === 'udp') return '17';
  if (value === 'icmp') return '1';
  if (value === 'all') return value;
  const parsed = Number(value);
  if (Number.isInteger(parsed) && parsed >= 0 && parsed <= 255) return String(parsed);
  return 'all';
}

function securityProtocolOptionValue(option) {
  return [option.value, option.minPort ?? '', option.maxPort ?? ''].join('|');
}

function securityProtocolOptionsHtml(selected) {
  return SECURITY_PROTOCOL_OPTIONS.map(option => {
    const value = securityProtocolOptionValue(option);
    return `<option value="${escapeAttr(value)}" ${value === selected ? 'selected' : ''}>${escapeHtml(option.label)}</option>`;
  }).join('');
}

function parseSecurityProtocolSelection(value) {
  const [protocol, minPort, maxPort] = String(value || 'all').split('|');
  return {
    protocol: protocol || 'all',
    minPort: minPort === '' ? null : Number(minPort),
    maxPort: maxPort === '' ? null : Number(maxPort)
  };
}

function icmpTypeOptionsHtml(selected) {
  const selectedValue = selected === null || selected === undefined ? '' : String(selected);
  return ICMP_TYPE_OPTIONS.map(option => `
    <option value="${escapeAttr(option.value)}" ${option.value === selectedValue ? 'selected' : ''}>${escapeHtml(option.label)}</option>
  `).join('');
}

function icmpCodeOptionsHtml(type, selected) {
  const typeValue = type === null || type === undefined ? '' : String(type);
  const selectedValue = selected === null || selected === undefined ? '' : String(selected);
  const options = ICMP_CODE_OPTIONS[typeValue] || ICMP_CODE_OPTIONS[''];
  return options.map(option => `
    <option value="${escapeAttr(option.value)}" ${option.value === selectedValue ? 'selected' : ''}>${escapeHtml(option.label)}</option>
  `).join('');
}

function protocolDisplayName(protocol) {
  if (protocol === 'all') return '所有协议';
  if (protocol === '1') return 'ICMP';
  if (protocol === '6') return 'TCP';
  if (protocol === '17') return 'UDP';
  return `协议 ${protocol}`;
}

function securityRuleAllowText(rule) {
  const protocol = normalizeSecurityListProtocol(rule.protocol);
  if (protocol === 'all') return '所有端口的所有流量';
  if (protocol === '6' || protocol === '17') {
    const minPort = rule.minPort ?? '';
    const maxPort = rule.maxPort ?? '';
    if (minPort === '' && maxPort === '') return `以下端口的 ${protocolDisplayName(protocol)} 流量：全部`;
    const endPort = maxPort === '' ? minPort : maxPort;
    return `以下端口的 ${protocolDisplayName(protocol)} 流量：${minPort}-${endPort}`;
  }
  if (protocol === '1') {
    const type = rule.icmpType ?? '';
    const code = rule.icmpCode ?? '';
    if (type === '' && code === '') return '以下项的 ICMP 流量：全部';
    if (code === '') return `ICMP 类型 ${type}：全部代码`;
    return `ICMP 类型 ${type}，代码 ${code}`;
  }
  return `${protocolDisplayName(protocol)} 流量`;
}

function addSecurityListRuleRow() {
  const list = selectedSecurityList();
  if (!list) return;

  const body = document.getElementById('sg-modal-body');
  if (body.querySelector('.empty-state')) body.innerHTML = '';
  const index = body.querySelectorAll('.sg-rule-row').length;
  body.insertAdjacentHTML('beforeend', securityListRuleRowHtml({
    protocol: '6',
    source: '0.0.0.0/0',
    destination: '0.0.0.0/0',
    sourceType: 'CIDR_BLOCK',
    destinationType: 'CIDR_BLOCK',
    minPort: 22,
    maxPort: 22,
    isStateless: false
  }, index));
  updateSecurityListProtocolControls();
}

function collectSecurityListRules() {
  const rows = document.getElementById('sg-modal-body').querySelectorAll('.sg-rule-row');
  return Array.from(rows).map(row => {
    const selectedProtocol = parseSecurityProtocolSelection(row.querySelector('.sg-f-protocol')?.value || 'all');
    const protocol = selectedProtocol.protocol;
    const minPort = securityRuleNumberValue(row.querySelector('.sg-f-min-port')?.value);
    const maxPort = securityRuleNumberValue(row.querySelector('.sg-f-max-port')?.value);
    const icmpType = securityRuleNumberValue(row.querySelector('.sg-f-icmp-type')?.value);
    const icmpCode = securityRuleNumberValue(row.querySelector('.sg-f-icmp-code')?.value);
    const endpoint = row.querySelector('.sg-f-endpoint')?.value?.trim() || '';
    const endpointType = row.querySelector('.sg-f-endpoint-type')?.value || 'CIDR_BLOCK';
    const rule = {
      id: row.dataset.ruleId || '',
      protocol,
      minPort: protocol === '6' || protocol === '17' ? minPort : null,
      maxPort: protocol === '6' || protocol === '17' ? maxPort : null,
      icmpType: protocol === '1' ? icmpType : null,
      icmpCode: protocol === '1' ? icmpCode : null,
      description: row.querySelector('.sg-f-description')?.value?.trim() || '',
      isStateless: row.querySelector('.sg-f-stateless')?.checked || false
    };
    if (securityListModalDirection === 'egress') {
      rule.destination = endpoint;
      rule.destinationType = endpointType;
    } else {
      rule.source = endpoint;
      rule.sourceType = endpointType;
    }
    return rule;
  });
}

function securityRuleNumberValue(value) {
  const trimmed = String(value || '').trim();
  if (trimmed === '') return null;
  const parsed = Number(trimmed);
  return Number.isFinite(parsed) ? parsed : null;
}

async function saveSecurityListRules() {
  const vm = securityListModalVM;
  const list = selectedSecurityList();
  const msgEl = document.getElementById('sg-modal-message');
  if (!vm || !list) return;

  msgEl.textContent = '保存中...';
  msgEl.className = 'form-message';

  try {
    const currentRules = collectSecurityListRules();
    const path = securityListModalResourceType === 'network-security-group'
      ? `/api/vm/${encodeURIComponent(vm.provider)}/${encodeURIComponent(vm.accountId)}/${encodeURIComponent(vm.id)}/network-security-groups/${encodeURIComponent(list.id)}/rules`
      : `/api/vm/${encodeURIComponent(vm.provider)}/${encodeURIComponent(vm.accountId)}/${encodeURIComponent(vm.id)}/security-lists/${encodeURIComponent(list.id)}/rules`;
    await fetchJSON(path, {
      method: 'POST',
      body: {
        ingressRules: securityListModalDirection === 'ingress' ? currentRules : (list.ingressRules || []),
        egressRules: securityListModalDirection === 'egress' ? currentRules : (list.egressRules || [])
      }
    });
    msgEl.textContent = '安全规则已保存。';
    msgEl.className = 'form-message success';
    addLog(`OCI 安全规则已保存：${list.name || list.id}`, 'success');
    loadSecurityLists(vm, list.id);
  } catch (err) {
    msgEl.textContent = err.message;
    msgEl.className = 'form-message error';
  }
}

function updateSecurityListProtocolControls() {
  document.querySelectorAll('.sg-rule-row').forEach(row => {
    const protocol = parseSecurityProtocolSelection(row.querySelector('.sg-f-protocol')?.value || 'all').protocol;
    const portDisabled = protocol !== '6' && protocol !== '17';
    const icmpDisabled = protocol !== '1';
    row.querySelectorAll('.sg-f-min-port, .sg-f-max-port').forEach(input => {
      input.disabled = portDisabled;
    });
    row.querySelectorAll('.sg-f-icmp-type, .sg-f-icmp-code').forEach(input => {
      input.disabled = icmpDisabled;
    });
  });
}

function applySecurityProtocolPreset(row) {
  if (!row) return;
  const selected = parseSecurityProtocolSelection(row.querySelector('.sg-f-protocol')?.value || 'all');
  const minPort = row.querySelector('.sg-f-min-port');
  const maxPort = row.querySelector('.sg-f-max-port');
  if ((selected.protocol === '6' || selected.protocol === '17') && selected.minPort !== null) {
    if (minPort) minPort.value = selected.minPort;
    if (maxPort) maxPort.value = selected.maxPort ?? selected.minPort;
  }
  if (selected.protocol !== '1') {
    const icmpType = row.querySelector('.sg-f-icmp-type');
    const icmpCode = row.querySelector('.sg-f-icmp-code');
    if (icmpType) icmpType.value = '';
    if (icmpCode) icmpCode.innerHTML = icmpCodeOptionsHtml('', '');
  }
}

function updateSecurityRuleDirectionTabs() {
  document.querySelectorAll('.sg-direction-tab').forEach(tab => {
    tab.classList.toggle('active', tab.dataset.direction === securityListModalDirection);
  });
}

function updateSecurityResourceTabs() {
  document.querySelectorAll('.sg-resource-tab').forEach(tab => {
    tab.classList.toggle('active', tab.dataset.resourceType === securityListModalResourceType);
  });
}

async function createNetworkSecurityGroupForVM() {
  const vm = securityListModalVM;
  const msgEl = document.getElementById('sg-modal-message');
  if (!vm) return;

  const name = document.getElementById('sg-new-nsg-name')?.value?.trim() || '';
  msgEl.textContent = '创建网络安全组中...';
  msgEl.className = 'form-message';

  try {
    const data = await fetchJSON(`/api/vm/${encodeURIComponent(vm.provider)}/${encodeURIComponent(vm.accountId)}/${encodeURIComponent(vm.id)}/network-security-groups`, {
      method: 'POST',
      body: { name }
    });
    const created = data.networkSecurityGroup || {};
    msgEl.textContent = '网络安全组已创建并关联。';
    msgEl.className = 'form-message success';
    addLog(`OCI 网络安全组已创建并关联：${created.name || created.id || name}`, 'success');
    document.getElementById('sg-new-nsg-name').value = '';
    loadSecurityLists(vm, created.id || '');
  } catch (err) {
    msgEl.textContent = err.message;
    msgEl.className = 'form-message error';
  }
}

document.addEventListener('DOMContentLoaded', () => {
  document.getElementById('sg-modal-close')?.addEventListener('click', closeSecurityListModal);
  document.getElementById('sg-modal-add')?.addEventListener('click', addSecurityListRuleRow);
  document.getElementById('sg-modal-save')?.addEventListener('click', saveSecurityListRules);
  document.getElementById('sg-create-nsg')?.addEventListener('click', createNetworkSecurityGroupForVM);
  document.getElementById('sg-modal-list')?.addEventListener('change', event => {
    renderSecurityListRules(event.target.value);
  });
  document.querySelectorAll('.sg-resource-tab').forEach(tab => {
    tab.addEventListener('click', () => {
      securityListModalResourceType = tab.dataset.resourceType || 'security-list';
      updateSecurityResourceTabs();
      if (securityListModalVM) loadSecurityLists(securityListModalVM);
    });
  });
  document.querySelectorAll('.sg-direction-tab').forEach(tab => {
    tab.addEventListener('click', () => {
      securityListModalDirection = tab.dataset.direction || 'ingress';
      updateSecurityRuleDirectionTabs();
      const list = selectedSecurityList();
      renderSecurityListRules(list ? list.id : '');
    });
  });
  document.getElementById('security-list-modal')?.addEventListener('click', event => {
    if (event.target.id === 'security-list-modal') closeSecurityListModal();
  });
  document.getElementById('sg-modal-body')?.addEventListener('click', event => {
    const btn = event.target.closest('.sg-rule-delete-btn');
    if (!btn) return;
    btn.closest('.sg-rule-row')?.remove();
    if (!document.getElementById('sg-modal-body').querySelector('.sg-rule-row')) {
      document.getElementById('sg-modal-body').innerHTML = `<div class="empty-state compact">暂无${securityListModalDirection === 'egress' ? '出站' : '入站'}规则</div>`;
    }
  });
  document.getElementById('sg-modal-body')?.addEventListener('change', event => {
    const protocolSelect = event.target.closest('.sg-f-protocol');
    if (protocolSelect) {
      applySecurityProtocolPreset(protocolSelect.closest('.sg-rule-row'));
      updateSecurityListProtocolControls();
    }
    const icmpTypeSelect = event.target.closest('.sg-f-icmp-type');
    if (icmpTypeSelect) {
      const row = icmpTypeSelect.closest('.sg-rule-row');
      const codeSelect = row?.querySelector('.sg-f-icmp-code');
      if (codeSelect) codeSelect.innerHTML = icmpCodeOptionsHtml(icmpTypeSelect.value, '');
    }
  });
});
