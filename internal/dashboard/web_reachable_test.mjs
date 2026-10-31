import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
import test from 'node:test';

const source=readFileSync(new URL('web/app.js',import.meta.url),'utf8');
const html=readFileSync(new URL('web/index.html',import.meta.url),'utf8');

// Exercise the shipped event handlers and action dispatcher without contacting
// Apple. Unrelated startup polling and rendering are outside this fixture.
function fixture(respond){
  const elements=new Map(),requests=[];
  function element(id=''){
    const classes=new Set();
    return {id,value:'',hidden:false,disabled:false,readOnly:false,textContent:'',children:[],listeners:{},
      classList:{add:c=>classes.add(c),remove:c=>classes.delete(c),toggle:(c,on)=>on?classes.add(c):classes.delete(c),contains:c=>classes.has(c)},
      addEventListener(name,fn){this.listeners[name]=fn;},
      querySelector(){return this.button??=element();},
      append(...items){this.children.push(...items);},
      replaceChildren(...items){this.children=items;},
      focus(){this.focused=true;},
      reportValidity(){return this.value.includes('@')&&!this.value.includes(' ');}
    };
  }
  for(const match of html.matchAll(/<[^>]+\bid="([^"]+)"[^>]*>/g)){
    const item=element(match[1]);item.hidden=/\shidden(?:\s|>)/.test(match[0]);elements.set(item.id,item);
  }
  const context=vm.createContext({
    document:{getElementById:id=>{assert.ok(elements.has(id),`missing DOM id ${id}`);return elements.get(id);},querySelector:()=>({content:'test-token'}),querySelectorAll:()=>[],createElement:()=>element(),body:element()},
    localStorage:{getItem:()=>'',setItem(){},removeItem(){}},
    location:{hash:'#overview'},setTimeout:()=>0,clearTimeout(){},
    confirm(){throw new Error('Sending must not depend on a native confirmation dialog');},
    fetch:async(url,options)=>{const request=JSON.parse(options.body);requests.push(request);return {status:200,ok:true,json:async()=>respond(request)};}
  });
  vm.runInContext(source.slice(0,source.indexOf("window.addEventListener('hashchange'")),context);
  vm.runInContext("poll=async()=>{};selectedAccount='test-account';",context);
  return {context,requests,get:id=>elements.get(id),run:code=>vm.runInContext(code,context)};
}
const settle=()=>new Promise(resolve=>setImmediate(resolve));

test('send click dispatches once and shows progress, then the code form',async()=>{
  let complete;
  const f=fixture(()=>new Promise(resolve=>{complete=resolve;}));
  f.get('hmeReachableEmail').value=' target@example.test ';
  f.get('hmeReachableStartButton').listeners.click();
  await settle();
  assert.equal(f.requests.length,1);
  assert.equal(f.requests[0].code,'target@example.test');
  assert.match(f.get('hmeReachableStatus').textContent,/正在/);
  f.get('hmeReachableStartButton').listeners.click();
  assert.equal(f.requests.length,1,'duplicate send while pending');
  complete({ok:true,stage:'hme-reachable-code',message:'验证码已发送'});
  await settle();
  assert.equal(f.get('hmeReachableStatus').textContent,'验证码已发送');
  assert.equal(f.get('hmeReachableCodeForm').hidden,false);
  assert.equal(f.get('hmeReachableCode').focused,true);
});

test('missing management session offers inline authentication and clears its password',async()=>{
  const f=fixture(request=>request.password?{ok:true,stage:'hme-reachable-code',message:'已发送'}:{ok:false,stage:'hme-reachable-auth-required',message:'请验证当前 Apple 账号'});
  f.get('hmeReachableEmail').value='target@example.test';
  await f.run("action('hme-reachable-start',{code:'target@example.test'})");
  assert.equal(f.get('hmeReachableAuthForm').hidden,false);
  assert.equal(f.get('hmeReachablePassword').focused,true);
  f.get('hmeReachablePassword').value='test-only-password';
  const pending=f.get('hmeReachableAuthForm').listeners.submit({preventDefault(){}});
  assert.equal(f.get('hmeReachablePassword').value,'');
  await pending;
  assert.equal(f.requests.length,2);
  assert.equal(f.requests[1].password,'test-only-password');
  assert.equal(f.get('hmeReachableAuthForm').hidden,true);
  assert.equal(f.get('hmeReachableCodeForm').hidden,false);
});

test('invalid email never sends',()=>{
  const f=fixture(()=>{throw new Error('unexpected request');});
  f.get('hmeReachableEmail').value='invalid';
  f.get('hmeReachableStartButton').listeners.click();
  assert.equal(f.requests.length,0);
});

test('rejection and network errors remain visible beside the button',async()=>{
  for(const respond of [()=>({ok:false,message:'Apple 拒绝发送',stage:'hme-reachable-send'}),()=>{throw new Error('network unavailable');}]){
    const f=fixture(respond);
    await f.run("action('hme-reachable-start',{code:'target@example.test'})");
    assert.equal(f.get('hmeReachableStatus').hidden,false);
    assert.equal(f.get('hmeReachableStatus').classList.contains('error'),true);
    assert.equal(f.get('hmeReachableCodeForm').hidden,true);
    assert.equal(f.run('busy'),false);
  }
});

test('switching accounts clears authentication and verification UI',()=>{
  const f=fixture(()=>({}));
  f.get('hmeReachablePassword').value='test-only-password';
  f.get('hmeReachableEmail').readOnly=true;
  f.get('hmeReachableAuthForm').hidden=false;
  f.get('hmeReachableCodeForm').hidden=false;
  f.run('resetHME()');
  assert.equal(f.get('hmeReachablePassword').value,'');
  assert.equal(f.get('hmeReachableEmail').readOnly,false);
  assert.equal(f.get('hmeReachableAuthForm').hidden,true);
  assert.equal(f.get('hmeReachableCodeForm').hidden,true);
  assert.equal(f.get('hmeReachableStatus').hidden,true);
});
