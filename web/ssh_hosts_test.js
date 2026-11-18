'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const elements = {};
for (const name of ['[data-ssh-status]','[data-ssh-list]','[data-ssh-candidates]','[data-ssh-save]','[data-ssh-file]','[data-ssh-import]','[data-ssh-marker]','[data-ssh-preview]','[data-ssh-manual]']) elements[name] = {textContent:'',innerHTML:'',hidden:false,disabled:false,value:'',querySelectorAll:()=>[]};
const scanButton={disabled:false};
const form={elements:{host:{value:'git.example'},port:{value:'22'}},querySelector:()=>scanButton};
elements['[data-ssh-scan]']=form;
const card={querySelector:name=>elements[name]};
const host={querySelector:name=>name==='#platformSSHHostsCard'?card:null};
let checked=[], writes=0, confirms=0;
elements['[data-ssh-candidates]'].querySelectorAll=()=>checked;
const candidate={host:'git.example',port:22,marker:'',keyType:'ssh-ed25519',fingerprint:'SHA256:example',publicKey:'ssh-ed25519 test'};
const ctx={state:{},document:{querySelector:()=>null},window:{confirm:()=>{confirms++;return true}},escapeHTML:v=>String(v),lfT:(a)=>a,
  api:async (url,opts)=>{
    if(url.endsWith('/preview'))return {items:[{candidate}]};
    if(url.endsWith('/scan'))throw new Error('扫描失败，请手动导入');
    if(opts?.method==='POST'){writes++;return {ok:true}}
    return {hosts:[]};
  }};
vm.createContext(ctx);
vm.runInContext(fs.readFileSync(process.argv[2],'utf8')+'\n;globalThis.__ssh={platformSSHHostsHTML,loadPlatformSSHHosts};',ctx);
ctx.__ssh.loadPlatformSSHHosts(host);
(async()=>{
  await elements['[data-ssh-preview]'].onclick();
  assert.match(elements['[data-ssh-candidates]'].innerHTML,/SHA256:example/);
  await elements['[data-ssh-save]'].onclick();
  assert.equal(writes,0);assert.equal(confirms,0);
  checked=[{dataset:{sshCandidate:'0'}}];
  await elements['[data-ssh-save]'].onclick();
  assert.equal(confirms,1);assert.equal(writes,1);
  await form.onsubmit({preventDefault(){},currentTarget:form});
  assert.match(elements['[data-ssh-status]'].textContent,/扫描失败/);
  assert.equal(elements['[data-ssh-save]'].hidden,true);
})().catch(err=>{console.error(err);process.exitCode=1});
