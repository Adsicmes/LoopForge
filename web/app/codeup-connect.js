// Codeup (Aliyun Yunxiao) repository picker. The GitHub picker can lean on an
// OAuth connection; Codeup has none, so this uses the caller's personal access
// token, which already lives in the Git credential slot. Yunxiao scopes every
// repository call by organization, so the organization id is part of the form:
// it is taken from whatever Codeup address is already typed, and can be edited.
function codeupOrganizationFromAddress(raw){
  const info=parseGongfengRepoURL(raw);
  if(info?.provider!=='codeup')return '';
  return String(info.project||'').replace(/^codeup:/,'').split('/')[0]||'';
}
function mountCodeupRepoPicker(host,onSelect,options){
  if(!host)return;
  const getAddress=typeof options?.getAddress==='function'?options.getAddress:()=>'';
  const configured=(userById(state.currentUser?.id)?.gitTokenProviders||[]).includes('codeup');
  host.innerHTML='<div class="codeup-picker">'
    +'<p class="codeup-picker-hint" data-codeup-hint>'+(configured
      ?'云效仓库用你在个人配置里的个人访问令牌读取，按组织范围列出。'
      :'还没有配置云效令牌：先在「个人配置 → 云效 Codeup」填写个人访问令牌（需要「代码管理 · 合并请求」读写权限）。')+'</p>'
    +'<label class="pa-field"><span>云效组织 ID</span><input class="pa-input" data-codeup-org placeholder="24 位十六进制，如 60de7a6852743a5162b5f957" autocomplete="off"></label>'
    +'<label class="pa-field"><span>搜索仓库</span><input class="pa-input" type="search" data-codeup-search placeholder="仓库名或分组，留空列出全部" autocomplete="off"></label>'
    +'<div class="codeup-picker-actions"><button type="button" class="button ghost small" data-codeup-load>从云效选择仓库</button></div>'
    +'<label class="pa-field" data-codeup-select-field hidden><span>选择仓库</span><select class="pa-input" data-codeup-repo aria-label="云效仓库"></select></label>'
    +'<p class="codeup-picker-error" data-codeup-error hidden></p>'
    +'</div>';
  const orgInput=host.querySelector('[data-codeup-org]'),searchInput=host.querySelector('[data-codeup-search]');
  const load=host.querySelector('[data-codeup-load]'),field=host.querySelector('[data-codeup-select-field]');
  const select=host.querySelector('[data-codeup-repo]'),error=host.querySelector('[data-codeup-error]');
  let repositories=[],busy=false;
  const showError=message=>{error.textContent=message||'';error.hidden=!message};
  const fillOrg=()=>{
    if(orgInput.value.trim())return;
    const org=codeupOrganizationFromAddress(getAddress());
    if(org)orgInput.value=org;
  };
  fillOrg();
  const paint=()=>{
    if(!repositories.length){field.hidden=true;return}
    select.replaceChildren(new Option('请选择仓库',''));
    for(const repository of repositories){
      const option=document.createElement('option');
      option.value=repository.project;
      option.textContent=(repository.name||repository.project)+' · '+repository.project.replace(/^codeup:/,'');
      option.dataset.repo=JSON.stringify(repository);
      select.append(option);
    }
    field.hidden=false;
  };
  select.onchange=()=>{
    const option=select.selectedOptions?.[0];
    if(!option?.dataset.repo)return;
    try{onSelect(JSON.parse(option.dataset.repo))}catch(_){}
  };
  const run=async()=>{
    if(busy)return;
    const org=orgInput.value.trim();
    showError('');
    if(!org){showError('先填云效组织 ID，或直接粘贴一个 Codeup 仓库地址');orgInput.focus();return}
    busy=true;load.disabled=true;load.textContent='读取中…';
    const actor=Number(state.currentUser?.id);
    try{
      const query=new URLSearchParams({org});
      const search=searchInput.value.trim();
      if(search)query.set('search',search);
      const result=await api('/api/codeup/repositories?'+query.toString());
      if(!host.isConnected||Number(state.currentUser?.id)!==actor)return;
      repositories=(result?.repositories||[]).filter(item=>item&&item.project);
      paint();
      if(!repositories.length)showError('这个组织下没有匹配的仓库（也可能是令牌没有该仓库的读权限）');
    }catch(err){
      repositories=[];paint();showError(err?.message||'读取云效仓库失败');
    }finally{
      busy=false;load.disabled=false;load.textContent='从云效选择仓库';
    }
  };
  // The organization is re-read at click time instead of tracking every
  // keystroke in the address field: no listener to leak when the drawer closes.
  load.onclick=()=>{fillOrg();return run()};
  searchInput.onkeydown=event=>{if(event.key==='Enter'){event.preventDefault();fillOrg();void run()}};
}
