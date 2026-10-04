const assert = require("node:assert/strict");
const {test} = require("node:test");
const {pathToFileURL} = require("node:url");
const path = require("node:path");
const modulePath = pathToFileURL(path.resolve(__dirname,"../assets/fusion/model.mjs")).href;
const model = () => import(modulePath);
const routes = [
 {id:"a",revision:1,model:"model-a",account:"account-a",efforts:["medium","high"],default_effort:"medium",no_effort:false},
 {id:"b",revision:2,model:"model-b",account:"account-b",efforts:[],default_effort:null,no_effort:true},
];
const locked = (r=routes[0]) => ({mode:"locked",route:{id:r.id,revision:r.revision},model:r.model,effort:{mode:r.no_effort?"none":"default"}});
test("expands groups then complete role overrides; explicit inherit masks group", async()=>{
 const m=await model();const layer={groups:{implementation_testing:locked()},roles:{testing:{mode:"inherit"}}};
 const out=m.expandLayer(layer);assert.equal(out.roles.implementation.model,"model-a");assert.deepEqual(out.roles.testing,{mode:"inherit"});
 assert.deepEqual(Object.keys(out.roles),m.roles);out.roles.implementation.route.id="changed";assert.equal(layer.groups.implementation_testing.route.id,"a");
});
test("independent testing and acceptance survive five-role serialize/reload",async()=>{
 const m=await model();const layer=m.expandLayer({roles:{implementation:locked(),testing:locked(routes[1]),review:locked(),acceptance:locked(routes[1])}});
 assert.equal(m.sameBinding(layer.roles.implementation,layer.roles.testing),false);
 const saved=m.expandLayer(JSON.parse(JSON.stringify(layer)));assert.equal(saved.roles.testing.model,"model-b");assert.equal(saved.roles.acceptance.route.revision,2);
});
test("collapsed visual grouping never rewrites role bindings",async()=>{
 const m=await model();const layer=m.expandLayer({roles:{implementation:locked(),testing:locked(routes[1])}});
 const before=JSON.stringify(layer);assert.equal(m.groupShared(layer,"implementation_testing"),false);assert.equal(JSON.stringify(layer),before);
 const merged=m.shareGroup(layer,"implementation_testing");assert.deepEqual(merged.roles.testing,merged.roles.implementation);assert.equal(JSON.stringify(layer),before);
 merged.roles.testing.route.id="changed";assert.equal(merged.roles.implementation.route.id,"a");
});
test("unknown modes, roles and groups are rejected rather than discarded",async()=>{
 const m=await model();for(const layer of [{roles:{other:{mode:"inherit"}}},{groups:{other:locked()}},{roles:{design:null}}])assert.throws(()=>m.expandLayer(layer));
 assert.equal(m.layerIssues({roles:{design:{mode:"unknown"}}},routes)[0].code,"mode_invalid");
});
test("expired exact route stays visible in draft, never falls back",async()=>{
 const m=await model();const layer=m.expandLayer({roles:{testing:locked(routes[1])}});
 assert.equal(m.layerIssues(layer,[routes[0]])[0].code,"route_unavailable");assert.equal(layer.roles.testing.route.id,"b");
});
test("all effort modes require declared capability; null is unspecified",async()=>{
 const m=await model();for(const [effort,expected] of [[null,"effort_unspecified"],[{mode:"explicit",value:"low"},"effort_unsupported"],[{mode:"none"},"effort_unsupported"],[{mode:"default"},null],[{mode:"explicit",value:"high"},null]]) {
  const b=locked();b.effort=effort;const issues=m.layerIssues(m.expandLayer({roles:{design:b}}),routes);assert.equal(issues[0]?.code??null,expected);
 }assert.deepEqual(m.layerIssues(m.expandLayer({roles:{design:locked(routes[1])}}),routes),[]);
});
test("automatic candidates are explicit, exact, nonempty and unique",async()=>{
 const m=await model();const a=locked();const candidate={route:a.route,model:a.model,effort:a.effort};
 for(const [candidates,expected] of [[[],"candidates_missing"],[[candidate,candidate],"candidate_duplicate"],[[candidate],null]]) {
  const layer=m.expandLayer({roles:{review:{mode:"auto",candidates}}});assert.equal(m.layerIssues(layer,routes)[0]?.code??null,expected);
 }
});
test("model and route cannot splice account, version or another model",async()=>{
 const m=await model();for(const changed of [{model:"model-b"},{route:{id:"a",revision:2}}]) {
  const b={...locked(),...changed};assert.equal(m.layerIssues(m.expandLayer({roles:{design:b}}),routes)[0].code,"route_unavailable");
 }
 assert.equal(m.layerIssues(m.expandLayer({roles:{design:locked()}}),[routes[0],routes[0]])[0].code,"route_unavailable");
});
test("route selection starts with unspecified effort, not a guessed default",async()=>{
 const m=await model();const b=m.targetFor(routes[0]);assert.deepEqual(b,{route:{id:"a",revision:1},model:"model-a",effort:null});assert.throws(()=>m.targetFor({...routes[0],revision:Number.MAX_SAFE_INTEGER+1}));
});
test("draft saving validation does not promote unadmitted route",async()=>{
 const m=await model();const list=routes.map(x=>({...x,admitted:false,billing_known:false,lock_enforcement:"unverified"}));
 assert.deepEqual(m.layerIssues(m.expandLayer({roles:{design:locked()}}),list),[]);assert.equal(list[0].admitted,false);
});
