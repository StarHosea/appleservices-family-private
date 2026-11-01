'use strict';
const $=id=>document.getElementById(id), token=document.querySelector('meta[name="app-token"]').content;
let loginMode='reconnect',loginAccount='',current=null,busy=false,privateCode='',privateCodeKind='',hideAt=0,lastState='',lastQuery='',lastRefresh='',lastRequest='',noticeTimer,selectedAccount=localStorage.getItem('selected-account')||'',familyInfo=null,familyAuthAccount='',familyAuthOrganizer='',familyAccount='',familyCheckedAt='',familyStale=false,storageAccount='',storageCheckedAt='',planAccount='',planCheckedAt='',poolOrganizerAccount=localStorage.getItem('family-organizer-account')||'',poolOrganizerName=localStorage.getItem('family-organizer-name')||'',purchaseAction='',purchaseLabel='',hmeInfo=null,hmeAccount='',hmeCandidate='',hmeFilter='active',hmeVisible=100,hmeEditing='',hmeReachableAction='',activationInfo=null,activationAccount='',autoRefreshScheduled=false;
const autoRefreshState=new Map();
const hmeReadAttempts=new Set();
const activationAttempts=new Set();
let loginFormReady=false,loginSubmitting=false,loginStartedAccount='',loginReturnRoute='#overview',loginResumePending=false,loginVerificationState='',loginActivationStarted=false;
const autoRefreshOwner=(globalThis.crypto?.randomUUID?.()||Math.random().toString(36).slice(2));
function autoRefreshLeaseKey(account){return 'appleservices:auto-refresh:'+account;}
function autoRefreshAttemptKey(key){return 'appleservices:auto-attempt:'+key;}
function acquireAutoRefreshLease(account){try{const key=autoRefreshLeaseKey(account),now=Date.now();let lease={};try{lease=JSON.parse(localStorage.getItem(key)||'{}');}catch{}if(lease.owner&&lease.owner!==autoRefreshOwner&&Number(lease.expires)>now)return false;localStorage.setItem(key,JSON.stringify({owner:autoRefreshOwner,expires:now+150000}));return JSON.parse(localStorage.getItem(key)||'{}').owner===autoRefreshOwner;}catch{return true;}}
function renewAutoRefreshLease(account){try{const key=autoRefreshLeaseKey(account),lease=JSON.parse(localStorage.getItem(key)||'{}');if(lease.owner===autoRefreshOwner)localStorage.setItem(key,JSON.stringify({owner:autoRefreshOwner,expires:Date.now()+150000}));}catch{}}
function releaseAutoRefreshLease(account){try{const key=autoRefreshLeaseKey(account),lease=JSON.parse(localStorage.getItem(key)||'{}');if(lease.owner===autoRefreshOwner)localStorage.removeItem(key);}catch{}}
function autoRefreshAttemptStatus(key){try{const value=JSON.parse(localStorage.getItem(autoRefreshAttemptKey(key))||'{}');return value.status||'';}catch{return '';}}
function autoRefreshAttempted(key){return !!autoRefreshAttemptStatus(key);}
function markAutoRefreshAttempt(key,status='refreshing'){try{localStorage.setItem(autoRefreshAttemptKey(key),JSON.stringify({status,owner:autoRefreshOwner,at:Date.now()}));}catch{}}
function clearAutoRefreshAttempt(key){try{localStorage.removeItem(autoRefreshAttemptKey(key));}catch{}}
const time=value=>{if(!value||value.startsWith('0001'))return '—';return new Intl.DateTimeFormat('zh-CN',{hour:'2-digit',minute:'2-digit',timeZone:'Asia/Singapore',hour12:false}).format(new Date(value));};
async function api(path,data){const suffix=path==='view'&&!selectedAccount?'':(selectedAccount?'?account='+encodeURIComponent(selectedAccount):'');const response=await fetch('/api/'+path+suffix,{method:data===undefined?'GET':'POST',headers:{'X-App-Token':token,'Content-Type':'application/json'},body:data===undefined?undefined:JSON.stringify(data),cache:'no-store'});if(response.status===401){location.assign('/login');throw new Error('登录已过期，请重新登录');}if(!response.ok)throw new Error(response.status===403?'请刷新面板后重试':'操作未完成，请稍后重试');return response.json();}
function notice(message,error=false){$('notice').textContent=message;$('notice').classList.toggle('error',error);$('notice').hidden=false;clearTimeout(noticeTimer);noticeTimer=setTimeout(()=>{$('notice').hidden=true;},6500);}
function activity(text,when=new Date().toISOString()){const list=$('activityList');list.querySelector('.empty-activity')?.remove();const entry=document.createElement('div');entry.className='activity-entry';const label=document.createElement('span');label.textContent=text;const stamp=document.createElement('time');stamp.textContent=time(when);entry.append(label,stamp);list.prepend(entry);while(list.children.length>4)list.lastElementChild.remove();}
function clearCode(){privateCode='';hideAt=0;$('codeDisplay').classList.remove('shown');$('codeDisplay').textContent='•••  •••';$('codeDetail').textContent=current?.code_ready?'点击按钮显示六位验证码':'登录后可生成六位验证码';$('copyButton').disabled=true;}
function showCode(result){privateCode=result.code;privateCodeKind=result.code_kind||'';hideAt=new Date(result.hide_at).getTime();if(!Number.isFinite(hideAt))hideAt=Date.now()+60000;$('codeDisplay').textContent=privateCode.slice(0,3)+' '+privateCode.slice(3);$('codeDisplay').classList.add('shown');$('copyButton').disabled=false;tickCode();if(result.message)notice(result.message);}
function tickCode(){if(!privateCode)return;const seconds=Math.max(0,Math.ceil((hideAt-Date.now())/1000));if(!seconds){clearCode();return;}$('codeDetail').textContent=(privateCodeKind==='circle'?'官方客户端配对码 · ':'')+seconds+' 秒后自动隐藏';}
function deviceIcon(device){const label=(device.model+' '+device.name).toLowerCase();if(label.includes('windows'))return '<svg viewBox="0 0 40 40" aria-hidden="true"><path d="M7 8h12v12H7z" fill="#82a7d8"/><path d="M21 8h12v12H21z" fill="#7897c9"/><path d="M7 22h12v12H7z" fill="#8fbad1"/><path d="M21 22h12v12H21z" fill="#729cbb"/></svg>';if(label.includes('iphone')||label.includes('ipad'))return '<svg viewBox="0 0 40 40" aria-hidden="true"><rect x="12" y="3" width="17" height="34" rx="4" fill="#617997"/><rect x="14" y="5" width="13" height="30" rx="3" fill="#c5d5e4"/><path d="M14 22L27 8v27H14z" fill="#a6bfce"/><rect x="18" y="6" width="5" height="2" rx="1" fill="#617997"/></svg>';return '<svg viewBox="0 0 40 40" aria-hidden="true"><rect x="7" y="7" width="26" height="20" rx="2" fill="#7389a0"/><rect x="9" y="9" width="22" height="16" rx="1" fill="#b6cedc"/><path d="M9 25L31 9v16z" fill="#88b2c4"/><path d="M7 28h26l4 4H3z" fill="#a9b9c9"/><path d="M3 32h34v1H3z" fill="#6e849b"/></svg>';}
function renderDevices(devices,status=current?.status||{},view=current||{}){
  const grid=$('deviceGrid'),checkedAt=view.devices_checked_at||status.last_device_query_at;
  const checked=!!checkedAt&&!checkedAt.startsWith('0001'),stage=status.device_query_stage||'';
  const needsLogin=stage==='device_identity_refresh_required';
  const count=devices?.length||0;
  $('deviceCount').textContent=$('navCount').textContent=checked||count?String(count):'—';
  $('devicesReconnectButton').hidden=!needsLogin&&(status.running||!view.icloud_activated);
  $('devicesReconnectButton').disabled=busy;
  const detail=needsLogin?'设备管理凭据已失效或缺失，请重新连接当前账号。':stage==='device_query_failed'?'设备列表读取失败，请稍后点击刷新设备重试。':'';
  $('deviceUpdate').textContent=detail+(view.devices_stale&&checked?' 显示上次成功读取的记录 · '+time(checkedAt):checked?' 最近同步 '+time(checkedAt)+' · '+count+' 台设备':detail?'':'设备列表与 Apple 账户同步');
  grid.replaceChildren();
  if(!count){
    const empty=document.createElement('div');empty.className='empty-state';
    empty.textContent=detail||(checked?'Apple 最近一次查询返回 0 台设备':status.running?'设备列表尚未读取成功，请点击刷新设备':view.icloud_activated?'账号已激活，重新连接后可读取设备列表':'登录后，你的设备会显示在这里');
    grid.append(empty);return;
  }
  for(const device of [...devices].sort((a,b)=>Number(b.installation)-Number(a.installation))){const card=document.createElement('article');card.className='device-card'+(device.installation?' installation':'');const icon=document.createElement('div');icon.className='device-icon';icon.innerHTML=deviceIcon(device);const info=document.createElement('div');info.className='device-info';const name=document.createElement('h3');name.textContent=device.name||'Apple 设备';const model=document.createElement('p');model.textContent=device.model||'已关联设备';info.append(name,model);const tags=document.createElement('div');tags.className='device-tags';if(device.installation){const tag=document.createElement('span');tag.className='device-tag blue';tag.textContent='本机登记';tags.append(tag);}if(device.trusted===true){const tag=document.createElement('span');tag.className='device-tag green';tag.textContent='受信任';tags.append(tag);}if(device.current&&!device.installation){const tag=document.createElement('span');tag.className='device-tag';tag.textContent='当前认证设备';tags.append(tag);}if(tags.children.length)info.append(tags);card.append(icon,info);grid.append(card);}
}
function gib(value){return value?(value/1073741824).toLocaleString('zh-CN',{maximumFractionDigits:0})+'GB':'—';}
function autoRefreshKey(kind,checkedAt){return selectedAccount+'|'+kind+'|'+(checkedAt||'none');}
function cacheLabel(kind,checkedAt,stale){if(!stale)return '已保存 · '+(checkedAt?time(checkedAt):'刚刚');const key=autoRefreshKey(kind,checkedAt),state=autoRefreshState.get(key)||autoRefreshAttemptStatus(key);if(state==='refreshing')return '正在后台核验 Apple…';if(state==='failed')return '自动核验失败 · '+(checkedAt?time(checkedAt):'刚刚');return '等待后台核验 · '+(checkedAt?time(checkedAt):'刚刚');}
function renderStorage(info,{checkedAt='',stale=false}={}){storageAccount=selectedAccount;storageCheckedAt=checkedAt||'';const note=$('storageCacheNote');if(!info){$('storageSummary').textContent='登录后查询当前容量和使用情况。';note.textContent='尚无本地状态';note.classList.remove('stale');return;}const format=x=>(x/1073741824).toLocaleString('zh-CN',{maximumFractionDigits:2})+' GB';$('storageSummary').textContent='已用 '+format(info.used_bytes)+' / '+format(info.total_bytes)+' · 可用 '+format(info.available_bytes);note.textContent=cacheLabel('storage',checkedAt,stale);note.classList.toggle('stale',!!stale);}
function updateFamilyCacheMeta(checkedAt='',stale=false,hasInfo=!!familyInfo){familyCheckedAt=checkedAt||'';familyStale=!!stale;const shell=$('familyCard');shell.classList.toggle('cache-stale',familyStale);if(!hasInfo){$('familyCacheNote').textContent='尚无本地状态';$('familyRefreshButton').textContent='读取家庭状态';return;}const key=autoRefreshKey('family',familyCheckedAt),state=autoRefreshState.get(key)||autoRefreshAttemptStatus(key);$('familyCacheNote').textContent=cacheLabel('family',familyCheckedAt,familyStale);$('familyRefreshButton').textContent=state==='refreshing'?'正在核验…':'重新核验 Apple';}
function renderFamilyMemberAccounts(info){
  const panel=$('familyMemberAccounts'),list=$('familyMemberList');list.replaceChildren();panel.hidden=!info?.member;
  if(!info?.member)return;
  if(!info.member_accounts?.length){const empty=document.createElement('li');empty.className='family-members-empty';empty.textContent='点击“重新核验 Apple”读取家庭成员账号。';list.append(empty);return;}
  const members=[...info.member_accounts].sort((a,b)=>Number(b.organizer)-Number(a.organizer)||Number(b.self)-Number(a.self));
  for(const member of members){
    const row=document.createElement('li');row.className='family-member-row';row.classList.toggle('organizer',!!member.organizer);
    const account=document.createElement('span');account.className='family-member-account';account.textContent=member.apple_id||'Apple 未返回此成员账号';
    const tags=document.createElement('span');tags.className='family-member-tags';
    const role=document.createElement('span');role.className='family-member-role';role.textContent=member.organizer?'组织者（母号）':'成员';tags.append(role);
    if(member.self){const self=document.createElement('span');self.className='family-member-self';self.textContent='当前账号';tags.append(self);}
    row.append(account,tags);list.append(row);
  }
}
let familyVerification=null,familyVerificationAccount='',familyVerificationSubmitting=false;
function renderFamilyVerification(state){
  const changed=familyVerificationAccount!==selectedAccount||familyVerification?.request_id!==state?.request_id||familyVerification?.stage!==state?.stage;
  familyVerification=state||null;familyVerificationAccount=selectedAccount;
  if(changed){familyVerificationSubmitting=false;$('familyVerificationCode').value='';}
  const visible=!!state&&state.stage!=='authenticating';
  $('familyVerificationPanel').hidden=!visible;
  if(!visible)return;
  $('familyAuthForm').hidden=true;
  const consent=state.stage==='repair-consent',code=state.stage==='sms-code';
  const expired=(consent||code)&&Date.parse(state.expires_at)<=Date.now();
  $('familyVerificationStatus').textContent=expired?'本次家庭验证已过期，请重新核验家庭状态后再加入。':consent?'Apple 要求继续短信验证。点击后向当前账号的绑定手机号发送验证码。':code?'Apple 已接受短信请求，请输入本次收到的六位验证码。':state.stage==='verifying-membership'?'正在核验家庭成员及共享储存状态，请稍候…':'正在等待 Apple 确认，请稍候…';
  $('familyVerificationSMS').hidden=!consent;$('familyVerificationSMS').disabled=expired||familyVerificationSubmitting;
  $('familyVerificationCodeForm').hidden=!code;$('familyVerificationCode').disabled=expired||familyVerificationSubmitting;$('familyVerificationCodeForm').querySelector('button').disabled=expired||familyVerificationSubmitting;
  $('familyVerificationCancel').disabled=expired||familyVerificationSubmitting||(!consent&&!code);
  if(changed&&code)$('familyVerificationCode').focus();
}
async function familyVerificationAction(name,code=''){
  const state=familyVerification,account=selectedAccount;
  if(!state||familyVerificationAccount!==account||familyVerificationSubmitting||Date.parse(state.expires_at)<=Date.now())return;
  familyVerificationSubmitting=true;
  $('familyVerificationSMS').disabled=true;$('familyVerificationCodeForm').querySelector('button').disabled=true;$('familyVerificationCancel').disabled=true;
  const input={action:name,request_id:state.request_id};if(code)input.code=code;
  try{
    const result=await api('action',input);
    if(account!==selectedAccount)return;
    if(!result.ok)throw new Error(result.message||'家庭验证未完成');
    $('familyVerificationStatus').textContent=result.message||'正在验证，请稍候…';
    await poll();
  }catch(error){if(account===selectedAccount){familyVerificationSubmitting=false;renderFamilyVerification(familyVerification);$('familyVerificationStatus').textContent=error.message;}}
  finally{input.code='';}
}
function resetFamilyAuth(){familyAuthAccount='';familyAuthOrganizer='';$('familyPassword').value='';$('familyAuthForm').hidden=true;$('familyAuthStatus').textContent='密码仅用于本次加入家庭，不保存；当前登录保持在线。';}
function renderFamily(info,{checkedAt='',stale=false,animate=true}={}){
  if(familyAccount&&familyAccount!==selectedAccount){$('familyActionStatus').hidden=true;$('familyActionStatus').textContent='';}if(familyAuthAccount&&(familyAuthAccount!==selectedAccount||info?.member))resetFamilyAuth();
  familyInfo=info||null;familyAccount=selectedAccount;
  const shell=$('familyCard'),card=$('familyStatus'),metrics=$('familyMetrics'),map=$('familyMap'),controls=$('familyControls'),removeControls=$('familyRemoveControls');card.className='family-status';
  updateFamilyCacheMeta(checkedAt,stale,!!info);
  renderFamilyMemberAccounts(info);
  if(!info){$('familySummary').textContent='尚未读取家庭状态';$('familyDetail').textContent='操作前会实时核对成员、组织者和共享储存状态。';$('familyStatusChip').textContent='待读取';metrics.hidden=true;map.hidden=true;controls.hidden=true;removeControls.hidden=true;return;}
  if(info.organizer_account){poolOrganizerAccount=info.organizer_account;poolOrganizerName=info.organizer_name||info.organizer_account;localStorage.setItem('family-organizer-account',poolOrganizerAccount);localStorage.setItem('family-organizer-name',poolOrganizerName);}
  metrics.hidden=false;
  controls.hidden=!!info.member;removeControls.hidden=!info.organizer;map.hidden=!info.member;
  if(info.member){card.classList.add(info.organizer?'organizer':'member');$('familySummary').textContent=info.organizer?'当前账号是家庭母号':'已加入家庭 · '+info.members+' 位成员';$('familyDetail').textContent=info.organizer?'组织者身份已由 Apple 实时回读确认。':'当前账号是普通成员，服务端已确认 self=1。';$('familyStatusChip').textContent=info.organizer?'母号':'家庭成员';}
  else if(info.eligibility_known&&info.eligible){card.classList.add('eligible');$('familySummary').textContent='尚未加入家庭 · 当前可加入';$('familyDetail').textContent='选择池内母号后提交；系统会双端回读，不以单次 HTTP 响应判定成功。';$('familyStatusChip').textContent='可加入';}
  else{$('familySummary').textContent='当前没有可确认的家庭成员资格';$('familyDetail').textContent=info.eligibility_known?'Apple 当前未确认可加入家庭。':'Apple 没有返回明确的家庭资格。';$('familyStatusChip').textContent='未确认';}
  $('familyMemberCount').textContent=info.members||'0';$('familyRole').textContent=info.organizer?'组织者':info.member?'成员':'未加入';
  $('familyStorageState').textContent=!info.storage_known?'未返回':info.storage_participating&&info.storage_available&&info.storage_enabled?'已启用':info.storage_available?'可加入':'未共享';
  $('familyStorageBytes').textContent=gib(info.storage_bytes);
  if(info.member){const selected=current?.accounts?.find(account=>account.id===selectedAccount);const currentName=info.member_accounts?.find(member=>member.self)?.apple_id||selected?.name||selectedAccount||'当前账号';$('familyOrganizerName').textContent=info.member_accounts?.find(member=>member.organizer)?.apple_id||info.organizer_name||info.organizer_account||'组织者账号尚未读取';$('familyCurrentName').textContent=currentName;$('familyCurrentAvatar').textContent=(currentName||'A').slice(0,1).toUpperCase();map.classList.toggle('self-organizer',!!info.organizer);}
  const sharing=[];sharing.push(info.purchase_sharing_known?(info.purchase_sharing_enabled?'购买共享已开启':'购买共享已关闭'):'未请求购买共享');sharing.push(info.location_sharing_known?(info.location_sharing_enabled?'位置共享已开启':'位置共享已关闭'):'未请求位置共享');$('familySafety').textContent=sharing.join(' · ')+'。家庭验证码只在 Apple 挑战出现后由当前账号自己的本地 Mac 身份生成。';
  if(animate){shell.classList.remove('family-updated');requestAnimationFrame(()=>shell.classList.add('family-updated'));window.setTimeout(()=>shell.classList.remove('family-updated'),700);}
}
function resetHME(){hmeInfo=null;hmeAccount='';hmeCandidate='';hmeVisible=100;hmeEditing='';$('hmeCandidatePanel').hidden=true;$('hmeReachablePanel').hidden=true;$('hmeReachableCodeForm').hidden=true;$('hmeReachableAuthForm').hidden=true;$('hmeReachablePassword').value='';$('hmeReachableStatus').hidden=true;$('hmeReachableEmail').readOnly=false;$('hmeReachableEmail').value='';$('hmeReachableCode').value='';$('hmeSearch').value='';$('hmeAuthForm').hidden=true;$('hmePassword').value='';$('hmeStatus').hidden=true;renderHME(null);}
function renderActivation(view){const ready=!!view?.activation_ready&&!view?.icloud_activated,card=$('activationCard');card.hidden=!ready;if(!ready)return;$('activationState').textContent=busy?'正在连接':'等待连接';$('activationSummary').textContent='登录后自动验证并保存 iCloud 服务会话；如 Apple 要求更新条款，将自动同意并继续连接。';$('activationVersions').hidden=true;$('activationICloudTerms').hidden=true;$('activationMediaTerms').hidden=true;$('activationCheckButton').textContent='重试连接';$('activationCheckButton').hidden=false;$('activationCheckButton').disabled=busy;$('activationAcceptButton').hidden=true;}
function hmeDate(value){if(!value)return '时间未知';const stamp=Number(value);const date=new Date(stamp<100000000000?stamp*1000:stamp);return Number.isNaN(date.getTime())?'时间未知':new Intl.DateTimeFormat('zh-CN',{year:'numeric',month:'2-digit',day:'2-digit'}).format(date);}
function showHMECandidate(candidate){hmeCandidate=candidate||'';$('hmeCandidateAddress').textContent=hmeCandidate||'—';$('hmeCandidatePanel').hidden=!hmeCandidate;if(hmeCandidate){$('hmeCreateLabel').value='';$('hmeCreateNote').value='';$('hmeCreateLabel').focus();}}
function hmeForwardOptions(info){const values=[info?.selected_forward_to,...(info?.forward_to_emails||[])].map(value=>(value||'').trim()).filter(Boolean);return values.filter((value,index)=>values.findIndex(item=>item.toLowerCase()===value.toLowerCase())===index);}
function reachableStatus(message,error=false){const status=$('hmeReachableStatus');status.textContent=message;status.hidden=false;status.classList.toggle('error',error);}
function renderHMEForwarding(info){const panel=$('hmeForwardSettings'),select=$('hmeForwardSelect'),selected=(info?.selected_forward_to||'').trim(),options=hmeForwardOptions(info);panel.hidden=!info;$('hmeReachablePanel').hidden=!info;select.replaceChildren();$('hmeForwardCurrent').textContent=selected||'Apple 未返回当前目标';for(const address of options){const option=document.createElement('option');option.value=address;option.textContent=address+(address.toLowerCase()===selected.toLowerCase()?' · 当前':'');select.append(option);}if(selected)select.value=selected;const hasAlternative=options.some(value=>value.toLowerCase()!==selected.toLowerCase());$('hmeForwardHint').textContent=hasAlternative?'所有隐私邮箱使用同一个目标；保存后会实时回读确认。':'当前账号没有其他已验证邮箱；可以在下方直接协议添加并验证。';select.disabled=!hasAlternative;$('hmeForwardButton').disabled=busy||!hasAlternative||!selected||select.value.toLowerCase()===selected.toLowerCase();}
function renderHME(info){hmeInfo=info||null;hmeAccount=selectedAccount;renderHMEForwarding(info);const list=$('hmeList');list.replaceChildren();$('hmeActiveCount').textContent=info?.active_count??'—';$('hmeInactiveCount').textContent=info?.inactive_count??'—';if(!info){$('hmeSummary').textContent='读取后可按标签、备注或邮箱搜索。';const empty=document.createElement('div');empty.className='hme-empty';empty.textContent='尚未读取当前子号的地址';list.append(empty);$('hmeMoreButton').hidden=true;return;}const query=$('hmeSearch').value.trim().toLowerCase();const aliases=(info.aliases||[]).filter(item=>(hmeFilter==='all'||(hmeFilter==='active')===!!item.active)&&(!query||[item.email,item.label,item.note,item.origin].some(value=>(value||'').toLowerCase().includes(query))));$('hmeSummary').textContent='共 '+(info.aliases?.length||0)+' 个地址 · '+(aliases.length===info.aliases?.length?'显示全部':'当前筛选 '+aliases.length+' 个');if(!aliases.length){const empty=document.createElement('div');empty.className='hme-empty';empty.textContent=query?'没有匹配的地址':'当前分类没有地址';list.append(empty);}for(const item of aliases.slice(0,hmeVisible)){const row=document.createElement('article');row.className='hme-row'+(item.active?'':' inactive');const identity=document.createElement('div');identity.className='hme-identity';const email=document.createElement('strong');email.textContent=item.email;const meta=document.createElement('small');meta.textContent=(item.label||'无标签')+' · '+hmeDate(item.create_timestamp);identity.append(email,meta);if(item.note){const note=document.createElement('p');note.textContent=item.note;identity.append(note);}const forward=document.createElement('div');forward.className='hme-forward';const state=document.createElement('span');state.className='hme-state '+(item.active?'active':'inactive');state.textContent=item.active?'转发中':'已停用';const destination=document.createElement('small');destination.textContent=item.forward_to_email?'转发至 '+item.forward_to_email:(item.origin||'iCloud+');forward.append(state,destination);const actions=document.createElement('div');actions.className='hme-actions';const copy=document.createElement('button');copy.className='icon-button';copy.title='复制邮箱';copy.setAttribute('aria-label','复制 '+item.email);copy.textContent='⧉';copy.addEventListener('click',async()=>{try{await navigator.clipboard.writeText(item.email);notice('邮箱地址已复制');}catch{notice('复制失败',true);}});const edit=document.createElement('button');edit.className='text-button';edit.textContent='编辑';edit.addEventListener('click',()=>{hmeEditing=item.email;$('hmeEditAddress').textContent=item.email;$('hmeEditLabel').value=item.label||'';$('hmeEditNote').value=item.note||'';$('hmeEditDialog').showModal();$('hmeEditLabel').focus();});actions.append(copy,edit);if(item.active){const deactivate=document.createElement('button');deactivate.className='text-button danger-text';deactivate.textContent='停用';deactivate.addEventListener('click',()=>{if(confirm('停用 '+item.email+'？停用后将停止转发邮件，但可以恢复。'))action('hme-deactivate',{code:item.email});});actions.append(deactivate);}else{const reactivate=document.createElement('button');reactivate.className='text-button';reactivate.textContent='恢复';reactivate.addEventListener('click',()=>action('hme-reactivate',{code:item.email}));const remove=document.createElement('button');remove.className='text-button danger-text';remove.textContent='永久删除';remove.addEventListener('click',()=>{if(confirm('永久删除 '+item.email+'？删除后无法恢复，也不能再使用这个地址。'))action('hme-delete',{code:item.email});});actions.append(reactivate,remove);}row.append(identity,forward,actions);list.append(row);}$('hmeMoreButton').hidden=aliases.length<=hmeVisible;}
function renderOrganizerOptions(accounts,selected){const select=$('familyOrganizer'),previous=select.value;select.replaceChildren();const placeholder=document.createElement('option');placeholder.value='';placeholder.textContent='选择母号';select.append(placeholder);for(const account of accounts){if(account.id===selected||!account.icloud_activated)continue;const option=document.createElement('option');option.value=account.id;option.textContent=(account.id===poolOrganizerAccount?'★ 家庭母号 · ':'')+(account.name||account.id)+' · '+account.id;select.append(option);}if([...select.options].some(option=>option.value===previous))select.value=previous;else if([...select.options].some(option=>option.value===poolOrganizerAccount))select.value=poolOrganizerAccount;}
function renderFamilyMemberOptions(accounts,organizerID){const select=$('familyRemoveMember'),previous=select.value;select.replaceChildren();const placeholder=document.createElement('option');placeholder.value='';placeholder.textContent='选择家庭成员';select.append(placeholder);for(const account of accounts){if(account.id===organizerID||!account.family_member||account.family_organizer||account.family_stale||account.family_organizer_account!==organizerID)continue;const option=document.createElement('option');option.value=account.id;option.textContent=(account.name||account.id)+' · '+account.id;select.append(option);}if([...select.options].some(option=>option.value===previous))select.value=previous;}
function renderAccounts(view){const list=$('accountList');if(!list)return;const accounts=view.accounts||[];const cachedOrganizers=accounts.filter(account=>account.family_organizer);if(cachedOrganizers.length===1){poolOrganizerAccount=cachedOrganizers[0].id;poolOrganizerName=cachedOrganizers[0].name||cachedOrganizers[0].id;localStorage.setItem('family-organizer-account',poolOrganizerAccount);localStorage.setItem('family-organizer-name',poolOrganizerName);}renderOrganizerOptions(accounts,view.selected);renderFamilyMemberOptions(accounts,view.selected);list.replaceChildren();for(const account of accounts){const organizer=account.family_organizer||account.id===poolOrganizerAccount;const item=document.createElement('button');item.type='button';item.className='account-item'+(account.id===view.selected?' active':'')+(organizer?' family-organizer':'')+(organizer&&account.family_stale?' stale-family':'');const avatar=document.createElement('span');avatar.className='account-avatar';avatar.textContent=organizer?'★':(account.name||'?').slice(0,1).toUpperCase();const copy=document.createElement('span');copy.className='account-item-copy';const strong=document.createElement('strong');strong.textContent=account.name||account.id;if(organizer){const role=document.createElement('span');role.className='account-role';role.textContent=account.family_stale?'母号待核验':'母号';strong.append(role);}const small=document.createElement('small');const state=account.running?(account.authenticated?'在线':'连接中'):account.icloud_activated?'iCloud 已激活':'已停止';small.textContent=organizer?'家庭组织者 · '+state:account.running&&account.authenticated?'在线工作区':state;copy.append(strong,small);const dot=document.createElement('i');dot.className=account.running&&account.authenticated?'on':account.running?'waiting':account.icloud_activated?'activated':'';item.append(avatar,copy,dot);item.addEventListener('click',()=>{if(loginSubmitting)return;if(isLoginPage())location.hash='overview';selectedAccount=account.id;localStorage.setItem('selected-account',account.id);familyInfo=null;familyAccount='';familyCheckedAt='';familyStale=false;storageAccount='';storageCheckedAt='';planAccount='';planCheckedAt='';purchaseAction='';purchaseLabel='';activationInfo=null;activationAccount='';$('purchaseButton').textContent='提交 50GB 方案';renderFamily(null);renderStorage(null);renderPlans(null);resetHME();clearCode();poll();});list.append(item);}}
function isLoginPage(){return ['#login','#login-add'].includes(location.hash);}
function verificationHelp(){const descriptions={'trusted-device':'在已登录此账户的 Apple 设备上获取验证码。',sms:'Apple 将向账户绑定的手机号发送一条新短信。','sms-existing':'使用已经收到、仍然有效的短信验证码，不会重新发送。',circle:'使用已登录此账户的受信任设备完成配对。'};$('verificationHelp').textContent=descriptions[$('twofactor').value]||'';}
function prepareLogin(mode,resumePending=false){
  loginMode=mode;loginAccount=selectedAccount;loginStartedAccount='';loginFormReady=true;loginResumePending=resumePending;loginVerificationState='';loginActivationStarted=false;
  $('appleID').value=mode==='reconnect'?(current?.login_account||''):'';
  $('appleID').readOnly=mode==='reconnect'&&!!$('appleID').value;
  $('password').value='';$('twofactor').value='trusted-device';$('loginError').hidden=true;
  $('loginTitle').textContent=mode==='add'?'添加 Apple 账户':'重新连接当前账号';
  $('loginDescription').textContent=mode==='add'?'添加一个账户，并完成 Apple 身份验证。':'使用当前账号的密码重新建立连接。';
  $('loginForm').setAttribute('action',mode==='add'?'/api/accounts/add':'/api/start');
  verificationHelp();
}
function openLogin(mode){
  if(busy)return;
  if(mode==='reconnect'&&!selectedAccount&&!(current?.accounts||[]).length)mode='add';
  if(!isLoginPage())loginReturnRoute=['#devices','#requests'].includes(location.hash)?location.hash:'#overview';
  prepareLogin(mode);
  location.hash=mode==='add'?'login-add':'login';applyRoute();
  ($('appleID').readOnly?$('password'):$('appleID')).focus();
}
function renderLogin(view){
  if(!isLoginPage())return;
  if(view?.selected&&view.selected!==selectedAccount)view=null;
  if(view&&!selectedAccount&&!(view.accounts||[]).length&&loginMode==='reconnect'&&!loginSubmitting){prepareLogin('add');history.replaceState(null,'','#login-add');}
  if(loginMode==='reconnect'&&!loginAccount)loginAccount=selectedAccount;
  if(loginMode==='reconnect'&&!$('appleID').value&&view?.login_account){$('appleID').value=view.login_account;$('appleID').readOnly=true;}
  const status=view?.status||{},waiting=['awaiting_sms','awaiting_existing_sms','awaiting_device_code','awaiting_account_validation','awaiting_pairing','awaiting_pairing_code','verifying_pairing'].includes(status.state);
  if(loginResumePending&&view){if(loginMode==='reconnect'&&waiting)loginStartedAccount=selectedAccount;loginResumePending=false;}
  let active=!!loginStartedAccount&&loginStartedAccount===selectedAccount;
  if(active&&view?.activation_ready)loginActivationStarted=true;
  const serviceCompleted=loginActivationStarted&&view?.icloud_activated;
  if(active&&!status.running&&!view?.launching&&view?.message&&!view?.activation_ready&&!serviceCompleted){$('loginError').textContent=view.message;$('loginError').hidden=false;loginStartedAccount='';active=false;}
  const completed=active&&(status.authenticated||serviceCompleted);
  $('loginFields').hidden=active;$('loginFields').disabled=busy;$('loginVerification').hidden=!active;if(completed)$('loginError').hidden=true;
  $('loginForm').setAttribute('aria-busy',String(loginSubmitting));
  $('loginSubmitButton').textContent=loginSubmitting?'正在提交…':'登录并继续 →';
  $('loginProgress').hidden=!active;
  $('loginProgressLabel').textContent=completed?'账户已登录':waiting?'完成身份验证':'正在连接 Apple…';
  $('loginProgressDescription').textContent=completed?'账户连接成功，后台将自动保持登录。':waiting?'在下方提交本次登录的验证码，或按提示完成设备配对。':'正在验证账户并建立连接，请稍候。';
  $('loginWorkspaceLink').hidden=!completed;$('loginRetryButton').hidden=!active||completed||busy;
  for(const [id,step] of [['loginStepCredentials',1],['loginStepVerification',2],['loginStepComplete',3]]){const currentStep=completed?3:active?2:1;$(id).classList.toggle('active',step===currentStep);$(id).classList.toggle('complete',step<currentStep);if(step===currentStep)$(id).setAttribute('aria-current','step');else $(id).removeAttribute('aria-current');}
  const verificationState=selectedAccount+'|'+status.state;
  if(active&&waiting&&loginVerificationState!==verificationState){loginVerificationState=verificationState;requestAnimationFrame(()=>{if(!isLoginPage())return;if(!$('smsPanel').hidden)$('smsCode').focus();else if(!$('pairingPanel').hidden&&!$('pairingCode').disabled)$('pairingCode').focus();});}
  for(const id of ['addAccountButton','addAccountButtonSecondary'])$(id).disabled=busy;
}
function scheduleAutoRefresh(view,activated){
  if(busy||autoRefreshScheduled||(isLoginPage()&&!loginStartedAccount))return;
  if(!activated){
    // The server continues this verified handoff even if this tab closes.
    if(view.status?.state==='awaiting_icloud_activation')return;
    const key=selectedAccount+'|activation|'+(view.status?.pid||'saved')+'|'+((view.status?.running?view.status?.last_refresh_at:view.status?.updated_at)||view.status?.state||'saved');
    if(view.activation_ready&&!view.launching&&(!view.status?.running||view.status?.authenticated)&&!activationAttempts.has(key)&&!autoRefreshAttempted(key)&&acquireAutoRefreshLease(selectedAccount)){
      activationAttempts.add(key);markAutoRefreshAttempt(key);autoRefreshScheduled=true;
      const account=selectedAccount;
      window.setTimeout(()=>{autoRefreshScheduled=false;if(account!==selectedAccount||busy){activationAttempts.delete(key);clearAutoRefreshAttempt(key);releaseAutoRefreshLease(account);return;}action('activation-connect',{}, {automatic:true,autoKey:key});},80);
    }
    return;
  }
  const candidates=[];
  if(!view.family||view.family_stale)candidates.push({kind:'family',action:'family-info',checkedAt:view.family_checked_at});
  if(view.storage&&view.storage_stale)candidates.push({kind:'storage',action:'storage-info',checkedAt:view.storage_checked_at});
  if(view.storage_plan&&view.storage_plan_stale)candidates.push({kind:'plan',action:'storage-plan',checkedAt:view.storage_plan_checked_at});
  if(!candidates.length){releaseAutoRefreshLease(selectedAccount);return;}
  const target=candidates.find(item=>{const key=autoRefreshKey(item.kind,item.checkedAt);return !autoRefreshState.has(key)&&!autoRefreshAttempted(key);});
  if(!target){releaseAutoRefreshLease(selectedAccount);return;}
  const account=selectedAccount,key=autoRefreshKey(target.kind,target.checkedAt);
  if(!acquireAutoRefreshLease(account))return;
  markAutoRefreshAttempt(key);
  autoRefreshState.set(key,'refreshing');autoRefreshScheduled=true;
  if(target.kind==='family')updateFamilyCacheMeta(view.family_checked_at,true,true);
  if(target.kind==='storage')renderStorage(view.storage,{checkedAt:view.storage_checked_at,stale:true});
  if(target.kind==='plan')renderPlans(view.storage_plan,{checkedAt:view.storage_plan_checked_at,stale:true});
  window.setTimeout(()=>{autoRefreshScheduled=false;if(account!==selectedAccount||busy){autoRefreshState.delete(key);clearAutoRefreshAttempt(key);releaseAutoRefreshLease(account);return;}renewAutoRefreshLease(account);action(target.action,{}, {silent:true,automatic:true,autoKey:key});},80);
}
function render(view){current=view;selectedAccount=view.selected||selectedAccount;renderFamilyVerification(view.family_authentication);
if(view.accounts)renderAccounts(view);if(view.family){const changed=familyAccount!==selectedAccount||!familyInfo||familyCheckedAt!==view.family_checked_at;if(changed)renderFamily(view.family,{checkedAt:view.family_checked_at,stale:view.family_stale,animate:false});else updateFamilyCacheMeta(view.family_checked_at,view.family_stale,true);}else if(familyAccount===selectedAccount&&familyInfo)renderFamily(null);if(view.storage){if(storageAccount!==selectedAccount||storageCheckedAt!==view.storage_checked_at)renderStorage(view.storage,{checkedAt:view.storage_checked_at,stale:view.storage_stale});}else if(storageAccount===selectedAccount&&storageCheckedAt)renderStorage(null);if(view.storage_plan){if(planAccount!==selectedAccount||planCheckedAt!==view.storage_plan_checked_at)renderPlans(view.storage_plan,{checkedAt:view.storage_plan_checked_at,stale:view.storage_plan_stale});}else if(planAccount===selectedAccount&&planCheckedAt)renderPlans(null);renderActivation(view);const status=view.status||{},online=status.running&&status.authenticated,activated=!!view.icloud_activated,waiting=status.verification_required,running=status.running||view.launching,starting=running&&!online&&!waiting;
const titles={initializing:'正在初始化本机身份',signing_in:'正在登录 Apple 账户',starting:'正在建立后台连接',refresh_failed:'登录刷新暂未完成',verification_required:'需要重新验证账户',account_repair_required:'客户端认证流程尚未完成',account_inactive:'Apple 账号未激活',awaiting_icloud_activation:'正在完成 iCloud 连接',awaiting_sms:'完成短信验证',awaiting_existing_sms:'输入已有短信验证码',awaiting_device_code:'输入设备验证码',awaiting_account_validation:'验证 Apple 账户',awaiting_pairing:'等待设备配对',awaiting_pairing_code:'输入设备配对码',verifying_pairing:'正在验证设备配对'};
$('sessionTitle').textContent=online?(status.push_issue==='reconnecting'?'登录正常 · 推送重连中':status.state==='refresh_failed'?'已登录 · 续期暂未完成':'登录持续在线'):(titles[status.state]||(status.state==='failed'&&!activated?'后台异常停止':starting?'正在连接 Apple':waiting?'需要短信验证':activated?'iCloud 已激活':'连接你的 Apple 账户'));
$('sessionDescription').textContent=status.state==='account_repair_required'?'Apple 返回了本客户端尚未支持的后续认证步骤，官网能正常登录也可能出现此状态。':online?(status.push_issue==='reconnecting'?'账户会话仍保留，后台正在恢复推送连接。':'设备身份已保留，后台会自动保持登录。'):waiting?(status.state==='awaiting_account_validation'?'请使用受信任 Apple 设备收到的验证码完成账户验证。':'请使用绑定手机号收到的验证码完成登录。'):starting?'正在恢复这台电脑的设备身份，请稍等。':activated?'Apple 已接受该账号的 iCloud 会话，可直接读取配额与可购方案。':status.state==='failed'?'后台运行出错，设备身份和已保存的会话仍保留。请重新连接。':'登录后，即可查看设备和生成本机验证码。';
const pairingStates={awaiting_approval:'收到官方客户端配对请求，请在登录请求中允许。',awaiting_client_proof:'等待官方客户端校验配对验证码。',code_exchanged:'配对证明已通过，已交换本机验证码；等待官方客户端结果。',exchange_complete_no_voucher:'验证码交换已完成。本机没有加密数据的信任凭据；官方验证结果仍需核实。',request_rejected:'配对请求参数未通过校验，尚未批准。',exchange_failed:'配对交换未完成，账户会话保留。',interrupted:'推送连接变化，原配对事务已终止，请重新发起验证。'};
const capabilities=[];if(status.push_issue==='token_changed')capabilities.push('推送凭据已变化，请停止后台后重新连接，以更新设备登记。');if(status.pairing_state)capabilities.push(pairingStates[status.pairing_state]||'正在处理客户端配对。');else if(status.pairing_unsupported)capabilities.push('后台是旧版本，尚不支持客户端配对，请更新并重启后台。');$('capabilityNotice').hidden=!capabilities.length;$('capabilityNotice').textContent=capabilities.join(' ');
$('statusBadge').className='status-badge'+(online?' online':waiting||starting?' waiting':'');$('statusBadge').replaceChildren();const dot=document.createElement('i');$('statusBadge').append(dot,document.createTextNode(online?'在线':waiting?'待验证':starting?'连接中':'未连接'));
$('authState').textContent=online?'已认证':activated?'iCloud 已激活':waiting?'等待验证':starting?'认证中':'未登录';$('pushState').textContent=status.push_connected?'已连接':status.push_issue==='reconnecting'?'重连中':'未连接';$('renewState').textContent=online?'每 10 分钟':activated?'按需验证':'等待登录';
for(const [id,active] of [['authDot',online],['pushDot',status.push_connected],['renewDot',online]])$(id).classList.toggle('active',!!active);
$('accountTag').hidden=!view.account;$('accountTag').textContent=view.account;$('sidebarAccount').replaceChildren(document.createTextNode(view.account||'本机账户'));const sub=document.createElement('small');sub.textContent='Windows 工作空间';$('sidebarAccount').append(sub);
// The account name and avatar are rendered from the account list (renderAccounts).
$('startButton').hidden=running;$('startButton').textContent=['failed','account_repair_required','account_inactive'].includes(status.state)?'重新连接':'连接账户';$('startButton').disabled=busy||starting;$('renewButton').hidden=!online;$('renewButton').disabled=busy||!view.control_ready;$('stopButton').hidden=!status.running;$('stopButton').disabled=busy;
$('sessionHint').textContent=online?'上次续期 '+time(status.last_refresh_at):activated?'已保存 Apple 验收会话':'登录身份会在本机保留';const storageRefreshing=autoRefreshAttemptStatus(autoRefreshKey('storage',view.storage_checked_at))==='refreshing',planRefreshing=autoRefreshAttemptStatus(autoRefreshKey('plan',view.storage_plan_checked_at))==='refreshing';$('codeButton').disabled=busy||!view.code_ready||!online;$('devicesButton').disabled=busy||!online||!view.control_ready;$('storageButton').disabled=busy||storageRefreshing||(!activated&&(!online||!view.control_ready));$('planButton').disabled=busy||planRefreshing||(!activated&&(!online||!view.control_ready));$('purchaseButton').disabled=busy||!online||!view.control_ready||!purchaseAction;$('storeAuthButton').disabled=busy||!online||!view.control_ready;$('storeAuthForm').querySelector('button').disabled=busy;
if(familyAuthAccount&&familyAuthAccount!==selectedAccount)resetFamilyAuth();$('familyPassword').disabled=busy||!online||familyStale;$('familyAuthForm').querySelector('button').disabled=busy||!online||familyStale||!view.control_ready;if(familyAccount&&familyAccount!==selectedAccount)renderFamily(null);const selectedFamily=familyAccount===selectedAccount?familyInfo:null,familyRefreshing=autoRefreshAttemptStatus(autoRefreshKey('family',view.family_checked_at))==='refreshing';$('familyRefreshButton').disabled=busy||familyRefreshing||(!activated&&!view.activation_ready);$('familyRefreshButton').title=activated?'查询 Apple 返回的家庭状态':'自动完成 iCloud 连接后读取家庭状态';if(!selectedFamily){$('familyDetail').textContent=activated?'点击右上角“读取家庭状态”，查询成员数量、当前角色和共享储存状态。':view.activation_ready?'账号已登录，正在自动完成 iCloud 连接并读取家庭状态。':'请先登录账号，再完成服务端 iCloud 连接以读取家庭状态。';}$('familyOrganizer').disabled=busy||!online||!!selectedFamily?.member||familyStale;$('familyAddButton').disabled=busy||!!view.family_authentication||!online||!view.control_ready||!!selectedFamily?.member||familyStale||!$('familyOrganizer').value;$('familyJoinStorageButton').disabled=busy||!activated||familyStale||!selectedFamily?.member||!selectedFamily?.storage_known||!selectedFamily?.storage_available||selectedFamily?.storage_participating;$('familyRemoveMember').disabled=busy||familyStale||!selectedFamily?.organizer;$('familyRemoveButton').disabled=busy||familyStale||!selectedFamily?.organizer||!$('familyRemoveMember').value;
const hmeEligible=!!selectedFamily?.member&&!selectedFamily?.organizer&&selectedFamily?.storage_known&&selectedFamily?.storage_participating&&selectedFamily?.storage_available&&selectedFamily?.storage_enabled;$('hmeCard').hidden=!hmeEligible;const hmeReady=hmeEligible&&!familyStale&&online&&view.control_ready&&view.hide_my_email_available;$('hmeListButton').disabled=busy||!hmeReady;$('hmeGenerateButton').disabled=busy||!hmeReady||!!hmeInfo?.generation_limited;$('hmeGenerateButton').textContent=hmeInfo?.generation_limited?'已达 Apple 上限':'＋ 生成候选';$('hmeGenerateButton').title=hmeInfo?.generation_limited?'Apple 已确认当前账号达到地址数量上限；地址减少后点击“读取地址”重新核验。':'';$('hmeReserveButton').disabled=busy||!hmeReady||!hmeCandidate||!$('hmeCreateLabel').value.trim();if(hmeEligible&&!view.hide_my_email_available&&!hmeInfo)$('hmeSummary').textContent=online?'当前后台版本不支持隐私邮箱，请重新连接此账号完成升级。':'重新连接此账号后可读取隐私邮箱。';if(hmeAccount&&hmeAccount!==selectedAccount)resetHME();
const forwardChoice=$('hmeForwardSelect').value,forwardCurrent=hmeInfo?.selected_forward_to||'';$('hmeForwardButton').disabled=busy||!hmeReady||!forwardChoice||forwardChoice.toLowerCase()===forwardCurrent.toLowerCase();
$('hmeReachableEmail').disabled=busy||!hmeReady||!$('hmeReachableCodeForm').hidden;$('hmeReachableStartButton').disabled=busy||!hmeReady||!$('hmeReachableEmail').value.trim()||!$('hmeReachableCodeForm').hidden;$('hmeReachableStartButton').textContent=hmeReachableAction==='hme-reachable-start'?'正在发送…':'发送邮箱验证码';$('hmeReachablePassword').disabled=busy||!hmeReady;$('hmeReachableAuthForm').querySelector('button').disabled=busy||!hmeReady;$('hmeReachableCode').disabled=busy;$('hmeReachableCodeForm').querySelector('button').disabled=busy;
$('smsPanel').hidden=!['awaiting_sms','awaiting_existing_sms','awaiting_device_code','awaiting_account_validation'].includes(status.state);const smsCode=['awaiting_sms','awaiting_existing_sms'].includes(status.state),deviceCode=!smsCode;$('smsPanel').querySelector('h3').textContent=deviceCode?'输入受信任设备上的验证码':'输入短信验证码';$('smsPanel').querySelector('p').textContent=deviceCode?'请在已登录该账号的 Apple 设备上获取六位验证码。':status.state==='awaiting_existing_sms'?'提交已经收到的短信码；本次不会再次发送短信。':'验证码已发送到账户绑定的手机号。';$('smsCode').placeholder=smsCode?'六位短信验证码':'六位设备验证码';$('smsCode').setAttribute('aria-label',$('smsCode').placeholder);$('resendButton').hidden=status.state!=='awaiting_sms';$('resendButton').disabled=busy;$('smsForm').querySelector('button[type="submit"]').disabled=busy;
$('pairingPanel').hidden=!['awaiting_pairing_code','verifying_pairing'].includes(status.state);$('pairingCode').disabled=busy||status.state==='verifying_pairing';$('pairingForm').querySelector('button').disabled=busy||status.state==='verifying_pairing';
renderDevices(view.devices,status,view);const pending=view.pending;$('requestDot').hidden=!pending;$('requestTag').textContent=pending?'待处理':'等待请求';$('requestTag').classList.toggle('pending',!!pending);$('requestTitle').textContent=pending?(pending.kind==='circle'?'官方客户端配对验证':'收到新的 Apple 登录请求'):'没有待处理的登录请求';$('requestText').textContent=pending?(pending.kind==='circle'?'允许后会显示配对码，请输入到发起验证的官方客户端。':'请确认这是你正在进行的登录。'):'收到新的请求时，它会出现在这里。';$('requestActions').hidden=!pending;$('allowButton').disabled=busy;$('denyButton').disabled=busy;
$('lastUpdated').textContent=running?'后台运行中 · '+time(status.updated_at):'后台未运行';if(!privateCode)$('codeDetail').textContent=view.code_ready?'点击按钮显示六位验证码':'登录后可生成六位验证码';
if(online&&lastState!==status.state)activity('后台登录已连接');if(!online&&lastState==='online'){activity('后台登录状态已变化');clearCode();}lastState=status.state;
if(status.last_device_query_at&&!status.last_device_query_at.startsWith('0001')&&lastQuery!==status.last_device_query_at){activity('设备列表已同步 · '+status.device_count+' 台',status.last_device_query_at);lastQuery=status.last_device_query_at;}
if(status.refresh_count&&lastRefresh!==status.last_refresh_at){activity('登录续期成功',status.last_refresh_at);lastRefresh=status.last_refresh_at;}
if(pending&&lastRequest!==pending.id){activity('收到新的登录请求',pending.received_at);lastRequest=pending.id;}
if(view.message&&!running)$('sessionDescription').textContent=view.message;
renderLogin(view);scheduleAutoRefresh(view,activated);
if(hmeReady&&!busy&&!autoRefreshScheduled&&!hmeInfo&&!hmeReadAttempts.has(selectedAccount)){const account=selectedAccount;hmeReadAttempts.add(account);window.setTimeout(()=>{if(account!==selectedAccount||busy){hmeReadAttempts.delete(account);return;}action('hme-list',{}, {silent:true});},0);}
}
let polling=false,pollQueued=false;
async function poll(){
  if(polling){
    // Account changes can arrive while the previous account's view is loading.
    // Run once more after it settles so the selected account is never stranded
    // behind a discarded response.
    pollQueued=true;
    return;
  }
  polling=true;
  const requested=selectedAccount;
  try{
    const view=await api('view');
    if(requested===selectedAccount)render(view);
  }catch(error){
    $('lastUpdated').textContent='面板连接暂时中断';
  }finally{
    polling=false;
    if(pollQueued){
      pollQueued=false;
      void poll();
    }
  }
}
function renderPlans(info,{checkedAt='',stale=false}={}){planAccount=selectedAccount;planCheckedAt=checkedAt||'';const list=$('planList'),note=$('planCacheNote');list.replaceChildren();purchaseAction='';purchaseLabel='';$('purchaseButton').textContent='提交 50GB 方案';if(!info?.plans?.length){$('planSummary').textContent=info?'Apple 没有返回可购方案。':'先读取 Apple 实时报价；仅已验证的币种与价格会启用购买按钮。';note.textContent=info?cacheLabel('plan',checkedAt,stale):'尚无本地状态';note.classList.toggle('stale',!!info&&!!stale);return;}note.textContent=cacheLabel('plan',checkedAt,stale);note.classList.toggle('stale',!!stale);let target=null;for(const plan of info.plans){const row=document.createElement('div');row.className='plan-row';const name=document.createElement('strong');name.textContent=(plan.bytes?gib(plan.bytes):'未知容量')+(plan.period?' · '+plan.period:'');const price=document.createElement('span');price.textContent=plan.currency?(plan.currency+' '+(plan.price_minor/100).toFixed(2)+'/月'):'价格待 Apple 返回';row.append(name,price);list.append(row);if(plan.bytes===50*1024*1024*1024&&plan.available)target=plan;}if(!stale&&target?.currency==='CNY'&&target.price_minor===600){purchaseAction='storage-purchase';purchaseLabel='CNY 6.00/月';}else if(!stale&&target?.currency==='USD'&&target.price_minor===99){purchaseAction='storage-skill-purchase-us';purchaseLabel='USD 0.99/月';}if(stale){$('planSummary').textContent='显示上次保存的报价，正在后台重新核验 Apple。';}else if(purchaseAction){$('purchaseButton').textContent='提交 50GB · '+purchaseLabel;$('planSummary').textContent='已核对 50GB · '+purchaseLabel+'。点击购买仍会再次确认。';}else{$('planSummary').textContent='Apple 返回了方案，但没有匹配已验证的 50GB 币种与价格；购买保持禁用。';}}
async function action(name,extra={},options={}){
  if(busy)return;
  if(name==='family-add-self'){$('familyActionStatus').hidden=false;$('familyActionStatus').textContent='正在验证并加入家庭…';$('familyActionStatus').classList.remove('error');}
  const reachableAction=name.startsWith('hme-reachable-');
  if(reachableAction)reachableStatus(name==='hme-reachable-start'?'正在验证账户并请求发送邮件，请稍候…':'正在验证邮箱并切换转发目标，请稍候…');
  const requestedAccount=selectedAccount,loginVerificationAction=isLoginPage()&&['submit-sms','submit-account-code','submit-pairing','resend-sms','activation-connect'].includes(name);
  if(loginVerificationAction)$('loginError').hidden=true;
  if(name.startsWith('hme-')&&familyStale){
    try{
      busy=true;document.body.classList.add('busy');if(current)render(current);
      const refreshed=await api('action',{action:'family-info'});
      if(requestedAccount!==selectedAccount)return;
      if(!refreshed.ok||!refreshed.family?.member||refreshed.family.organizer)throw new Error(refreshed.message||'子号家庭状态核验未完成');
      const checkedAt=new Date().toISOString();if(current){current.family=refreshed.family;current.family_checked_at=checkedAt;current.family_stale=false;}renderFamily(refreshed.family,{checkedAt,stale:false});
    }catch(error){if(reachableAction&&requestedAccount===selectedAccount)reachableStatus(error.message,true);notice(error.message,true);return;}
    finally{busy=false;document.body.classList.remove('busy');if(current)render(current);}
  }
  const familyAction=name.startsWith('family-'),hmeAction=name.startsWith('hme-');if(reachableAction)hmeReachableAction=name;busy=true;document.body.classList.add('busy');if(familyAction)$('familyCard').classList.add('is-loading');if(hmeAction)$('hmeCard').classList.add('is-loading');if(current)render(current);
  try{
    const result=await api('action',{action:name,...extra});
    if(requestedAccount!==selectedAccount)return;
    if(result.family){const checkedAt=new Date().toISOString();if(current){current.family=result.family;current.family_checked_at=checkedAt;current.family_stale=false;}renderFamily(result.family,{checkedAt,stale:false});}
    if(!result.ok){
      if(result.hide_my_email)renderHME(result.hide_my_email);
      const detail=(result.stage?' · '+result.stage:'')+(result.http_status?' · HTTP '+result.http_status:'')+(result.apple_hint?' · '+result.apple_hint:'');
	  if(name==='devices'&&result.stage==='device_identity_refresh_required'){
		notice(result.message||'设备管理登录态需要补齐',true);
		openLogin('reconnect');
		return;
	  }
	  if(name==='validate-account'&&result.stage==='reconnect-required'){
		notice(result.message||'请重新连接当前账号',true);
		openLogin('reconnect');
		return;
	  }
	  if(name==='family-add-self'&&result.stage==='family-password-required'){
 $('familyActionStatus').textContent=result.message||'请输入当前账号密码后继续';
		familyAuthAccount=requestedAccount;familyAuthOrganizer=extra.code||'';
		$('familyAuthForm').hidden=false;$('familyAuthStatus').textContent=result.message||'请输入当前账号密码，验证后继续加入家庭。';
		$('familyAuthForm').scrollIntoView?.({behavior:'smooth',block:'center'});
		return;
	  }
	  if(name==='hme-reachable-start'&&['hme-reachable-auth-required','reconnect-required'].includes(result.stage)){
        $('hmeReachableAuthForm').hidden=false;
		reachableStatus('请输入当前 Apple 账号密码后发送验证邮件；邮件尚未发送，密码不会保存。',true);
		$('hmeReachableAuthForm').scrollIntoView?.({behavior:'smooth',block:'center'});
		return;
      }
      if(name==='storage-authenticate'){
        $('storeAuthSummary').textContent=(result.message||'购买会话验证未完成')+detail;
        $('storeAuthForm').hidden=result.stage!=='purchase-store-2fa';
      }
      if(hmeAction){$('hmeStatus').textContent=(result.message||'隐藏邮件操作未完成')+detail;$('hmeStatus').hidden=false;if(result.stage==='hme-auth-required'){$('hmeAuthForm').hidden=false;$('hmePassword').focus();}}
      if(name==='storage-purchase')$('planSummary').textContent=(result.message||'购买请求未完成')+detail;
      throw new Error((result.message||'操作未完成')+detail);
    }
    if(name==='family-add-self'){resetFamilyAuth();$('familyActionStatus').hidden=false;$('familyActionStatus').textContent=result.message||'家庭操作已完成';}
    if(result.activation){activationInfo=result.activation;activationAccount=selectedAccount;if(current&&result.activation.completed){current.icloud_activated=true;current.activation_ready=false;}renderActivation(current);}
    if(result.store_authentication?.ready){$('storeAuthSummary').textContent='购买会话认证通过 · 当前后台会话可用 · 临时会话不会持久化';$('storeAuthForm').hidden=true;}
    if(result.storage){const checkedAt=new Date().toISOString();if(current){current.storage=result.storage;current.storage_checked_at=checkedAt;current.storage_stale=false;}renderStorage(result.storage,{checkedAt,stale:false});}
    if(result.storage_plan){const checkedAt=new Date().toISOString();if(current){current.storage_plan=result.storage_plan;current.storage_plan_checked_at=checkedAt;current.storage_plan_stale=false;}renderPlans(result.storage_plan,{checkedAt,stale:false});}
    if(result.hide_my_email){renderHME(result.hide_my_email);$('hmeAuthForm').hidden=true;$('hmeStatus').textContent=result.hide_my_email.generation_limited?'当前账号已有 '+result.hide_my_email.aliases.length+' 个隐藏邮件地址，Apple 已确认达到数量上限，暂不能生成新地址。地址数量减少后，点击“读取地址”重新核验。':result.hide_my_email.aliases?.length?'已读取 Apple 最新地址列表':'Apple 读取成功，当前账号尚无隐藏邮件地址；可以点击“生成候选”。';$('hmeStatus').hidden=false;}
	if(result.stage==='hme-reachable-code'){$('hmeReachableAuthForm').hidden=true;$('hmeReachableCodeForm').hidden=false;$('hmeReachableEmail').readOnly=true;}
	if(['hme-reachable-completed','hme-reachable-added'].includes(result.stage)){$('hmeReachableCodeForm').hidden=true;$('hmeReachableEmail').readOnly=false;$('hmeReachableCode').value='';}
    if(reachableAction)reachableStatus(result.message||'操作已完成');
    if(result.hme_candidate)showHMECandidate(result.hme_candidate);
    if(name==='hme-reserve')showHMECandidate('');
    if(result.family){if(name==='family-remove-member')activity('家庭成员已移出并完成双边核验');else if(result.family.member)activity('家庭成员状态已确认');if(result.family.storage_participating)activity('家庭共享储存已启用');}
    if(result.purchase){
      const messages={
        completed:'Apple 已确认购买完成。',
        'existing-paid-plan':'Apple 当前已返回至少 50GB 付费容量，未重复购买。',
        'verification-pending':'Apple 返回完成页面，付费容量尚未核实。',
        'authentication-required':'Apple 要求重新验证购买。',
        'confirmation-required':'Apple 要求进一步确认，订阅尚未完成。',
        handoff:'Apple 返回后续购买步骤，订阅尚未完成。'
      };
      $('planSummary').textContent=messages[result.purchase.state]||'购买状态尚未确认。';
    }
    if(name==='family-add-self'&&!result.family){
      const readback=await api('action',{action:'family-info'});
      if(!readback.ok||!readback.family?.member||readback.family.self!==1)throw new Error(readback.message||'家庭加入后的目标账号回读未完成');
      result.family=readback.family;let checkedAt=new Date().toISOString();if(current){current.family=result.family;current.family_checked_at=checkedAt;current.family_stale=false;}renderFamily(result.family,{checkedAt,stale:false});
      if(result.family.storage_known&&result.family.storage_available&&!result.family.storage_participating){
        const joined=await api('action',{action:'family-join-storage'});
        if(!joined.ok||!joined.family?.storage_participating||!joined.family?.storage_enabled)throw new Error(joined.message||'已加入家庭，但共享储存尚未生效');
        result.family=joined.family;checkedAt=new Date().toISOString();if(current){current.family=result.family;current.family_checked_at=checkedAt;current.family_stale=false;}renderFamily(result.family,{checkedAt,stale:false});result.message='池内账号已加入家庭并启用共享 iCloud+ 储存';
      }
    }
    if(options.autoKey)autoRefreshState.delete(options.autoKey);
    if(result.code)showCode(result);else if(!options.silent)notice(result.message||'操作已完成');
    await poll();
  }catch(error){if(name==='family-add-self'&&requestedAccount===selectedAccount){$('familyActionStatus').hidden=false;$('familyActionStatus').textContent=error.message;$('familyActionStatus').classList.add('error');}if(name==='family-add-self'&&familyAuthAccount===requestedAccount&&requestedAccount===selectedAccount)$('familyAuthStatus').textContent=error.message;if(reachableAction&&requestedAccount===selectedAccount)reachableStatus(error.message,true);if(options.autoKey){autoRefreshState.set(options.autoKey,'failed');markAutoRefreshAttempt(options.autoKey,'failed');}if(loginVerificationAction){$('loginError').textContent=error.message;$('loginError').hidden=false;}else if(!options.silent)notice(error.message,true);}
  finally{busy=false;if(reachableAction)hmeReachableAction='';if(name==='activation-connect')releaseAutoRefreshLease(requestedAccount);document.body.classList.remove('busy');if(familyAction)$('familyCard').classList.remove('is-loading');if(hmeAction)$('hmeCard').classList.remove('is-loading');if(current)render(current);if(name==='family-add-self'&&requestedAccount===selectedAccount&&!$('familyAuthForm').hidden)$('familyPassword').focus();if(reachableAction&&requestedAccount===selectedAccount){if(!$('hmeReachableAuthForm').hidden)$('hmeReachablePassword').focus();else if(!$('hmeReachableCodeForm').hidden)$('hmeReachableCode').focus();}}
}
$('reloadView').addEventListener('click',poll);$('codeButton').addEventListener('click',()=>action('code'));$('devicesButton').addEventListener('click',()=>action('devices'));$('devicesReconnectButton').addEventListener('click',()=>openLogin('reconnect'));$('renewButton').addEventListener('click',()=>action('validate-account'));
$('storageButton').addEventListener('click',()=>action('storage-info'));
$('activationCheckButton').addEventListener('click',()=>action('activation-connect'));
$('activationAcceptButton').addEventListener('click',()=>action('activation-connect'));
$('planButton').addEventListener('click',()=>action('storage-plan'));
$('familyRefreshButton').addEventListener('click',()=>action('family-info'));
$('familyVerificationSMS').addEventListener('click',()=>familyVerificationAction('family-auth-sms'));
$('familyVerificationCodeForm').addEventListener('submit',event=>{event.preventDefault();const code=$('familyVerificationCode').value.trim();$('familyVerificationCode').value='';if(/^[0-9]{6}$/.test(code))familyVerificationAction('family-auth-code',code);});
$('familyVerificationCancel').addEventListener('click',()=>familyVerificationAction('family-auth-cancel'));

$('familyOrganizer').addEventListener('change',()=>{resetFamilyAuth();if(current)render(current);});
$('familyRemoveMember').addEventListener('change',()=>{if(current)render(current);});
$('familyAddButton').addEventListener('click',()=>{const organizer=$('familyOrganizer').value;if(!organizer)return;const label=$('familyOrganizer').selectedOptions[0]?.textContent||organizer;if(!confirm('将当前账号加入 '+label+' 的家庭，并在可用时启用共享 iCloud+？购买共享和位置共享保持关闭。'))return;action('family-add-self',{code:organizer});});
$('familyAuthForm').addEventListener('submit',async event=>{
  event.preventDefault();if(busy)return;
  if(familyAuthAccount!==selectedAccount||!familyAuthOrganizer||familyAuthOrganizer!==$('familyOrganizer').value){resetFamilyAuth();return;}
  if(familyStale||familyInfo?.member||!current?.status?.running||!current?.status?.authenticated||!current?.control_ready)return;
  const credentials={code:familyAuthOrganizer,password:$('familyPassword').value};$('familyPassword').value='';
  if(!credentials.password)return;
  $('familyAuthStatus').textContent='正在验证并加入家庭，请稍候…';
  try{await action('family-add-self',credentials);}finally{credentials.password='';}
});
$('familyJoinStorageButton').addEventListener('click',()=>action('family-join-storage'));
$('familyRemoveButton').addEventListener('click',()=>{const target=$('familyRemoveMember').value;if(!target)return;const label=$('familyRemoveMember').selectedOptions[0]?.textContent||target;if(!confirm('确认将 '+label+' 移出当前家庭？该账号会立即失去共享 iCloud+，之后仍可重新拉入。购买共享和位置共享不会开启。'))return;action('family-remove-member',{code:target});});
$('hmeListButton').addEventListener('click',()=>action('hme-list'));
$('hmeAuthForm').addEventListener('submit',async event=>{event.preventDefault();if(busy)return;const account=selectedAccount,credentials={password:$('hmePassword').value};$('hmePassword').value='';try{if(account===selectedAccount)await action('hme-authenticate',credentials);}finally{credentials.password='';}});
$('hmeGenerateButton').addEventListener('click',()=>action('hme-generate'));
$('hmeForwardSelect').addEventListener('change',()=>{const choice=$('hmeForwardSelect').value,current=hmeInfo?.selected_forward_to||'';$('hmeForwardButton').disabled=busy||!choice||choice.toLowerCase()===current.toLowerCase();});
$('hmeForwardButton').addEventListener('click',()=>{const target=$('hmeForwardSelect').value,current=hmeInfo?.selected_forward_to||'';if(!target||target.toLowerCase()===current.toLowerCase())return;if(!confirm('确认将当前账号的所有隐私邮箱统一转发到 '+target+'？'))return;action('hme-forward',{code:target});});
$('hmeReachableEmail').addEventListener('input',()=>{if(current)render(current);});
$('hmeReachableStartButton').addEventListener('click',()=>{if(busy)return;const input=$('hmeReachableEmail');input.value=input.value.trim();if(!input.reportValidity())return;action('hme-reachable-start',{code:input.value});});
$('hmeReachableAuthForm').addEventListener('submit',async event=>{event.preventDefault();if(busy)return;const input=$('hmeReachableEmail');input.value=input.value.trim();if(!input.reportValidity())return;const credentials={code:input.value,password:$('hmeReachablePassword').value};$('hmeReachablePassword').value='';try{await action('hme-reachable-start',credentials);}finally{credentials.password='';}});
$('hmeReachableCodeForm').addEventListener('submit',event=>{event.preventDefault();const code=$('hmeReachableCode').value.trim();$('hmeReachableCode').value='';if(code)action('hme-reachable-verify',{code});});
$('hmeCreateLabel').addEventListener('input',()=>{if(current)render(current);});
$('hmeCancelCandidate').addEventListener('click',()=>showHMECandidate(''));
$('hmeReserveButton').addEventListener('click',()=>{const label=$('hmeCreateLabel').value.trim(),note=$('hmeCreateNote').value.trim();if(!hmeCandidate||!label)return;if(!confirm('确认创建 '+hmeCandidate+' 并开始转发邮件？'))return;action('hme-reserve',{code:hmeCandidate,label,note});});
$('hmeSearch').addEventListener('input',()=>{hmeVisible=100;renderHME(hmeInfo);});
for(const button of document.querySelectorAll('[data-hme-filter]'))button.addEventListener('click',()=>{hmeFilter=button.dataset.hmeFilter;hmeVisible=100;for(const item of document.querySelectorAll('[data-hme-filter]'))item.classList.toggle('active',item===button);renderHME(hmeInfo);});
$('hmeMoreButton').addEventListener('click',()=>{hmeVisible+=100;renderHME(hmeInfo);});
$('closeHMEEdit').addEventListener('click',()=>{$('hmeEditDialog').close();hmeEditing='';});
$('hmeEditDialog').addEventListener('close',()=>{hmeEditing='';});
$('hmeEditForm').addEventListener('submit',event=>{event.preventDefault();const email=hmeEditing,label=$('hmeEditLabel').value.trim(),note=$('hmeEditNote').value.trim();if(!email||!label)return;$('hmeEditDialog').close();action('hme-update',{code:email,label,note});});
$('purchaseButton').addEventListener('click',()=>{if(!purchaseAction||!purchaseLabel)return;if(!confirm('确认购买 iCloud+ 50GB（'+purchaseLabel+'）？这是会产生真实扣款并自动续费的订阅。'))return;action(purchaseAction);});$('storeAuthButton').addEventListener('click',()=>action('storage-authenticate'));$('storeAuthForm').addEventListener('submit',event=>{event.preventDefault();const code=$('storeCode').value;$('storeCode').value='';action('storage-authenticate',{code});});
$('addAccountButton').addEventListener('click',()=>openLogin('add'));$('addAccountButtonSecondary').addEventListener('click',()=>openLogin('add'));$('removeAccountButton').addEventListener('click',async()=>{if(busy)return;if(!selectedAccount){notice('当前是内置账号，先在左侧选择要移除的账号',true);return;}if(!confirm('从列表移除该账号？设备身份仍保留在磁盘上，可重新添加。'))return;busy=true;try{const result=await api('accounts/remove',{id:selectedAccount});if(!result.ok)throw new Error(result.message);notice(result.message);selectedAccount='';localStorage.removeItem('selected-account');}catch(error){notice(error.message,true);}finally{busy=false;await poll();}});
$('copyButton').addEventListener('click',async()=>{if(!privateCode)return;try{await navigator.clipboard.writeText(privateCode);notice('验证码已复制');}catch{notice('复制失败，请手动输入屏幕上的验证码',true);}});
$('allowButton').addEventListener('click',()=>{if(current?.pending)action('allow',{request_id:current.pending.id});});$('denyButton').addEventListener('click',()=>{if(current?.pending)action('deny',{request_id:current.pending.id});});
$('startButton').addEventListener('click',()=>openLogin('reconnect'));
$('twofactor').addEventListener('change',verificationHelp);
$('loginRetryButton').addEventListener('click',()=>{if(busy)return;prepareLogin('reconnect');if(current)renderLogin(current);$('password').focus();});
$('loginForm').addEventListener('submit',async event=>{
  event.preventDefault();if(busy)return;
  const mode=loginMode,account=loginAccount,creds={apple_id:$('appleID').value.trim(),password:$('password').value,twofactor:$('twofactor').value};
  busy=true;loginSubmitting=true;$('password').value='';$('loginError').hidden=true;if(current)renderLogin(current);
  try{
    if(mode==='reconnect'&&account!==selectedAccount)throw new Error('当前账号已切换，请重新选择要连接的账号');
    const result=await api(mode==='add'?'accounts/add':'start',creds);
    if(!result.ok)throw new Error(result.message||'登录未完成，请重试');
    if(mode==='add'){
      if(!result.account)throw new Error('服务未返回新增账号，请刷新后在账号列表中选择');
      selectedAccount=result.account;localStorage.setItem('selected-account',selectedAccount);
    }
    loginAccount=selectedAccount;loginStartedAccount=selectedAccount;loginMode='reconnect';
    if(location.hash==='#login-add')history.replaceState(null,'','#login');
    await poll();
  }catch(error){if(isLoginPage()){$('loginError').textContent=error.message;$('loginError').hidden=false;loginStartedAccount='';}}
  finally{busy=false;loginSubmitting=false;creds.password='';if(current)renderLogin(current);await poll();if(isLoginPage()&&!$('loginError').hidden)$('password').focus();}
});
$('smsForm').addEventListener('submit',event=>{event.preventDefault();const code=$('smsCode').value;$('smsCode').value='';action(current?.status?.state==='awaiting_account_validation'?'submit-account-code':'submit-sms',{code});});$('resendButton').addEventListener('click',()=>action('resend-sms'));
$('pairingForm').addEventListener('submit',event=>{event.preventDefault();const code=$('pairingCode').value;$('pairingCode').value='';action('submit-pairing',{code});});
$('stopButton').addEventListener('click',()=>{$('stopDialog').showModal();});$('cancelStop').addEventListener('click',()=>{$('stopDialog').close();});$('confirmStop').addEventListener('click',async()=>{if(busy)return;busy=true;$('stopDialog').close();try{const result=await api('stop',{account:selectedAccount});if(!result.ok)throw new Error(result.message);clearCode();notice(result.message);await poll();}catch(error){notice(error.message,true);}finally{busy=false;await poll();}});
function applyRoute(){
  const route=(location.hash||'#overview').slice(1),login=['login','login-add'].includes(route),page=login?'login':['overview','devices','requests'].includes(route)?route:'overview';
  document.querySelector('main').className='view-'+page;
  if(login){
    const mode=route==='login-add'?'add':'reconnect';
    if(!loginFormReady||loginMode!==mode)prepareLogin(mode,true);
    const label={'#devices':'返回我的设备','#requests':'返回登录请求','#overview':'返回概览'}[loginReturnRoute]||'返回概览';
    $('loginBackLink').setAttribute('href',loginReturnRoute);$('loginBackLink').textContent='← '+label;
  }else{$('password').value='';$('smsCode').value='';$('pairingCode').value='';loginFormReady=false;loginStartedAccount='';}
  const target=$(login?'loginVerification':'overviewVerification');for(const id of ['smsPanel','activationCard','pairingPanel'])target.append($(id));
  const headings={overview:['设备与登录','在一处管理设备身份、登录状态和验证码。'],devices:['我的设备','查看和刷新当前账号关联的设备。'],requests:['登录请求','处理 Apple 设备发来的登录确认。'],login:['账户登录','连接账户并完成身份验证。']};
  $('pageTitle').textContent=headings[page][0];$('pageDescription').textContent=headings[page][1];
  for(const item of document.querySelectorAll('nav a'))item.classList.toggle('selected',item.getAttribute('href')==='#'+page);
  if(current)renderLogin(current);
}
window.addEventListener('hashchange',applyRoute);applyRoute();
requestAnimationFrame(()=>requestAnimationFrame(()=>document.body.classList.add('ui-ready')));poll();setInterval(poll,2500);setInterval(tickCode,500);window.addEventListener('pagehide',()=>{clearCode();resetFamilyAuth();$('familyVerificationCode').value='';$('hmeReachablePassword').value='';$('password').value='';$('smsCode').value='';$('pairingCode').value='';});
