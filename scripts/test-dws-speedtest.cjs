const {test}=require('node:test');
const assert=require('node:assert/strict');
const vm=require('node:vm');
const fs=require('node:fs');
const path=require('node:path');
const html=fs.readFileSync(path.join(__dirname,'../webservice/speedtest.html'),'utf8');
const source=[...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].map(m=>m[1]).join('\n');
function setup(){
 const nodes=Object.fromEntries(['start','stop','status','latency','jitter','download','upload'].map(id=>[id,{disabled:false,textContent:''}]));
 const instances=[],requests=[],timers=new Map(),events={};let next=0,respond=async()=>({ok:true,json:async()=>({session:'test-token'})});
 class Speedtest{
 constructor(){this.params={};instances.push(this)}
 setParameter(k,v){this.params[k]=v}
 start(){this.worker={terminate:()=>this.terminated=true};this.updater=99}
 }
 vm.runInNewContext(source,{document:{getElementById:id=>nodes[id]},window:{addEventListener:(n,f)=>events[n]=f},location:{origin:'http://localhost',pathname:'/'},Speedtest,AbortSignal,encodeURIComponent,
 setTimeout:fn=>{timers.set(++next,fn);return next},clearTimeout:id=>timers.delete(id),clearInterval:()=>{},fetch:(url,options)=>{requests.push({url,options});return url.includes('dws_test=start')?respond():Promise.resolve({ok:true})}});
 return {nodes,instances,requests,timers,events,setResponse:fn=>respond=fn};
}
test('completion terminates worker, releases session and permits repeat',async()=>{
 const s=setup();await s.nodes.start.onclick();const t=s.instances[0];t.onupdate({testState:4,dlStatus:'100',ulStatus:'50',pingStatus:'2',jitterStatus:'1'});t.onend(false);
 assert.equal(t.terminated,true);assert.equal(s.nodes.start.disabled,false);assert.equal(s.timers.size,0);assert.match(s.requests.at(-1).url,/dws_test=stop&session=test-token/);
 await s.nodes.start.onclick();assert.equal(s.instances.length,2);
});
for(const mode of ['stop','error','messageerror','timeout','pagehide'])test(mode+' releases worker and session',async()=>{
 const s=setup();await s.nodes.start.onclick();const t=s.instances[0];
 if(mode==='stop')s.nodes.stop.onclick();if(mode==='error')t.worker.onerror({preventDefault(){}});if(mode==='messageerror')t.worker.onmessageerror();if(mode==='timeout')[...s.timers.values()][0]();if(mode==='pagehide')s.events.pagehide();
 assert.equal(t.terminated,true);assert.equal(s.nodes.start.disabled,false);assert.equal(s.timers.size,0);assert.match(s.requests.at(-1).url,/dws_test=stop/);
});
test('stop while waiting releases late grant without starting a worker',async()=>{
 const s=setup();let resolve;s.setResponse(()=>new Promise(r=>resolve=r));const pending=s.nodes.start.onclick();s.nodes.stop.onclick();resolve({ok:true,json:async()=>({session:'late-token'})});await pending;
 assert.equal(s.instances.length,0);assert.match(s.requests.at(-1).url,/session=late-token/);
});
test('busy admission recovers controls and displays retry message',async()=>{
 const s=setup();s.setResponse(async()=>({ok:false,status:429}));await s.nodes.start.onclick();assert.equal(s.nodes.start.disabled,false);assert.match(s.nodes.status.textContent,/稍后重试/);assert.equal(s.instances.length,0);
});
