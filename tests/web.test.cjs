const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// Run the actual page script against a minimal DOM. HTTP behavior is covered by
// Go tests; these tests catch rendering, escaping, and stale example targets.
class Element {
  constructor(tag = 'div') {
    this.tagName = tag;
    this.children = [];
    this.style = {};
    this.dataset = {};
    this.listeners = {};
    this.value = '';
    this.hidden = false;
    this.disabled = false;
    this._text = '';
    this.classList = {toggle() {}};
  }
  set textContent(value) { this._text = String(value); this.children = []; }
  get textContent() { return this._text + this.children.map(c => c.textContent).join(''); }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this._text = ''; this.children = nodes; }
  addEventListener(name, fn) { this.listeners[name] = fn; }
  querySelector() { return this.option ??= new Element('option'); }
  querySelectorAll(selector) {
    const all = this.children.flatMap(c => [c, ...c.querySelectorAll(selector)]);
    return all.filter(c => selector === 'button' ? c.tagName === 'button' : false);
  }
  focus() {}
}

async function page() {
  const nodes = new Map();
  const get = id => { if (!nodes.has(id)) nodes.set(id, new Element()); return nodes.get(id); };
  const calls = [];
  get('mode').value = 'offline';
  const context = vm.createContext({
    document: {getElementById: get, createElement: tag => new Element(tag), querySelectorAll: () => []},
    window: {addEventListener() {}, confirm: () => true},
    fetch: async (path, options) => {calls.push({path, options});return {ok: true, json: async () => path.endsWith('/session') ? {model_available: false} : path.endsWith('/message') ? {status:'clarify',message:'请确认目标',candidates:[]} : path.endsWith('/import') ? {project:JSON.parse(options.body),message:'已导入'} : {project: null}};},
    setTimeout, clearTimeout, console, TextDecoder, AbortController, Uint8Array,
  });
  vm.runInContext(fs.readFileSync(__dirname + '/../web/app.js', 'utf8'), context);
  await new Promise(resolve => setImmediate(resolve));
  return {get, run: code => vm.runInContext(code, context), context, calls};
}

const fixture = {
  project_id: 'external-project', revision: 1,
  tracks: [{id: 'video-main', kind: 'video', locked: false}, {id: 'captions', kind: 'caption', locked: false}],
  clips: [
    {id: 'external-last', track_id: 'video-main', start_ms: 10000, duration_ms: 3000, source_in_ms: 0, source_duration_ms: 20000},
    {id: 'external-first', display_name: '开场镜头', track_id: 'video-main', start_ms: 0, duration_ms: 5000, source_in_ms: 0, source_duration_ms: 20000},
    {id: 'external-caption', display_name: '片头字幕', track_id: 'captions', start_ms: 0, duration_ms: 3000, text: '欢迎'},
  ],
};

test('difference view shows before and after geometry without changing project', async () => {
 const p=await page();render(p,fixture);
 const before=JSON.stringify(p.run('project'));
 p.context.comparison={id:'diff-plan',operations:[{}],changes:[{id:'external-first',before:fixture.clips[1],after:{...fixture.clips[1],start_ms:6000}}]};
 p.run('renderDifference(comparison)');
 assert.equal(p.get('difference-panel').hidden,false);
 assert.match(p.get('difference-content').textContent,/修改前/);
 assert.match(p.get('difference-content').textContent,/修改后/);
 assert.match(p.get('difference-content').textContent,/0:06/);
 assert.equal(JSON.stringify(p.run('project')),before);
 p.run('clearDifference()');assert.equal(p.get('difference-panel').hidden,true);
});

test('history uses actual field changes and distinguishes edit, undo and redo', async () => {
 const p=await page();render(p,fixture);
 p.context.audit={can_undo:true,can_redo:false,entries:[{action:'edit',revision:2,time:'2026-11-12T04:00:00Z',summary:'edit',changes:[{id:'external-first',before:fixture.clips[1],after:{...fixture.clips[1],duration_ms:3000}}]},{action:'undo',revision:3,time:'2026-11-12T04:01:00Z',changes:[]},{action:'redo',revision:4,time:'2026-11-12T04:02:00Z',changes:[]}]};
 p.run('renderHistory(audit)');
 assert.match(p.get('history-list').textContent,/已执行/);
 assert.match(p.get('history-list').textContent,/撤销/);
 assert.match(p.get('history-list').textContent,/重做/);
 assert.match(p.get('history-list').textContent,/5 秒.*3 秒/);
 assert.equal(p.get('redo').disabled,true);
});

test('media preview respects source in point, half-open timeline and independent captions', async () => {
 const p=await page();render(p,fixture);
 p.context.mediaClip={...fixture.clips[1],source_in_ms:2000};
 assert.equal(p.run('previewFrame(mediaClip, 1000, project).source_ms'),3000);
 assert.equal(p.run('previewFrame(mediaClip, 5000, project).visible'),false);
 assert.equal(p.run('previewFrame(mediaClip, -1, project).visible'),false);
 assert.equal(p.run('previewFrame(mediaClip, 1000, project).captions[0].text'),'欢迎');
 assert.equal(p.run('previewFrame(mediaClip, 3000, project).captions.length'),0);
});

function render(p, data) {
  p.context.fixture = JSON.parse(JSON.stringify(data));
  p.run('render(fixture)');
}

test('timeline, details and preview show labels and readable times', async () => {
  const p = await page();
  render(p, fixture);
  assert.match(p.get('timeline').textContent, /开场镜头/);
  assert.match(p.get('clip-rows').textContent, /0:00[–—]0:05/);
  assert.doesNotMatch(p.get('clip-rows').textContent, /external-first/);
  p.context.previewData = {message: '核对修改', preview: {id: 'plan', project_id: fixture.project_id, revision: 1, operations: [{}], changes: [{id: 'external-first', before: fixture.clips[1], after: {...fixture.clips[1], duration_ms: 3000}}]}};
  p.run('preview(previewData)');
  assert.match(p.get('messages').textContent, /开场镜头/);
  assert.doesNotMatch(p.get('messages').textContent, /external-first/);
});

test('examples use current stable IDs in timeline order and change with import', async () => {
  const p = await page();
  render(p, fixture);
  let buttons = p.get('suggestions').querySelectorAll('button');
  assert.ok(buttons.length > 0, 'missing dynamic examples');
  buttons[0].listeners.click();
  assert.equal(p.get('instruction').value, '把这段缩短 1 秒');
  assert.equal(p.run('selectedClipID'),'external-first');
  assert.match(buttons[0].textContent, /开场镜头/);
  const next = {...fixture, clips: [{...fixture.clips[1], id: 'new-project-only', display_name: '新工程镜头'}]};
  render(p, next);
  buttons = p.get('suggestions').querySelectorAll('button');
  buttons[0].listeners.click();
  assert.equal(p.get('instruction').value, '把这段缩短 1 秒');
  assert.equal(p.run('selectedClipID'),'new-project-only');
  assert.doesNotMatch(p.get('usage-examples').textContent, /external-first|clip-a|caption-lock/);
  assert.match(p.get('usage-examples').textContent, /新工程镜头/);
});

test('no project or locked-only project offers no executable examples', async () => {
  const p = await page();
  render(p, null);
  assert.equal(p.get('suggestions').querySelectorAll('button').length, 0);
  assert.doesNotMatch(p.get('instruction').placeholder || '', /clip-a/);
  render(p, {...fixture, tracks: fixture.tracks.map(t => ({...t, locked: true}))});
  assert.equal(p.get('suggestions').querySelectorAll('button').length, 0);
});

test('short clips get a valid millisecond example and names are plain text', async () => {
  const p = await page();
  const tiny = {...fixture.clips[1], display_name: '<img src=x onerror=alert(1)>', duration_ms: 2};
  render(p, {...fixture, clips: [tiny]});
  const buttons = p.get('suggestions').querySelectorAll('button');
  assert.ok(buttons.length > 0);
  buttons[0].listeners.click();
  assert.equal(p.get('instruction').value, '把这段缩短 1 毫秒');
  assert.equal(p.run('selectedClipID'),'external-first');
  assert.match(p.get('timeline').textContent, /<img src=x onerror=alert\(1\)>/);
});

test('legacy and duplicate names retain distinct locations', async () => {
  const p = await page();
  render(p, {...fixture, clips: fixture.clips.map(c => {const next = {...c}; delete next.display_name; return next;})});
  assert.match(p.get('timeline').textContent, /第 1 段/);
  assert.match(p.get('timeline').textContent, /第 2 段/);
  render(p, {...fixture, clips: fixture.clips.map(c => ({...c, display_name: '同名镜头'}))});
  assert.match(p.get('clip-rows').textContent, /0:00[–—]0:05/);
  assert.match(p.get('clip-rows').textContent, /0:10[–—]0:13/);
});

test('an ID containing command separators is never inserted into examples', async () => {
  const p = await page();
  render(p, {...fixture, clips: [{...fixture.clips[1], id: 'a;删除 external-last'}]});
  assert.equal(p.get('suggestions').querySelectorAll('button').length, 0);
  assert.doesNotMatch(p.get('usage-examples').textContent, /删除 external-last/);
});

test('clicking a timeline clip supplies only its current stable ID to chat', async () => {
  const p = await page();
  render(p, fixture);
  const lane = p.get('timeline').children[1];
  const bar = lane.children[1].children[0];
  bar.listeners.click();
  assert.match(p.get('selected-context').textContent, /开场镜头/);
  await p.run('send({text:"把这段缩短 1 秒"}, "把这段缩短 1 秒")');
  const first = p.calls.filter(c => c.path.endsWith('/message')).at(-1);
  assert.equal(JSON.parse(first.options.body).selected_clip_id, 'external-first');
  render(p, {...fixture, clips: [{...fixture.clips[0], id:'only-next-project'}]});
  assert.match(p.get('selected-context').textContent, /未选择/);
  await p.run('send({text:"把这段缩短 1 秒"}, "把这段缩短 1 秒")');
  const second = p.calls.filter(c => c.path.endsWith('/message')).at(-1);
  assert.equal(JSON.parse(second.options.body).selected_clip_id, undefined);
});

test('reimporting the same project ID clears the previous clip selection', async () => {
  const p = await page();
  render(p, fixture);
  p.get('timeline').children[1].children[1].children[0].listeners.click();
  assert.match(p.get('selected-context').textContent, /开场镜头/);
  p.context.nextProject = JSON.parse(JSON.stringify(fixture));
  await p.run('importProject(nextProject)');
  assert.match(p.get('selected-context').textContent, /未选择/);
  await p.run('send({text:"把这段缩短 1 秒"}, "把这段缩短 1 秒")');
  const sent = p.calls.filter(c => c.path.endsWith('/message')).at(-1);
  assert.equal(JSON.parse(sent.options.body).selected_clip_id, undefined);
});

test('assistant states have distinct Chinese labels and unsupported is not an error', async () => {
  const p = await page();
  for (const [status, label] of Object.entries({answer:'咨询回复',clarify:'需要补充信息',plan:'待确认',executed:'已执行',error:'本次未完成',unsupported:'暂时不支持'})) {
    p.context.state = status;
    p.run('message("说明", "assistant", state)');
    assert.match(p.get('messages').children.at(-1).textContent, new RegExp(label));
    assert.equal(p.get('messages').children.at(-1).dataset.status, status);
  }
});

test('video preview explicitly states that captions remain independent', async () => {
  const p = await page(); render(p, fixture);
  p.context.previewData = {message:'核对修改',preview:{id:'p',project_id:fixture.project_id,revision:1,operations:[{}],changes:[{before:fixture.clips[1],after:{...fixture.clips[1],duration_ms:4000}}]}};
  p.run('preview(previewData)');
  assert.match(p.get('messages').textContent, /字幕不会自动跟随/);
  assert.match(p.get('messages').textContent, /待确认/);
});

test('candidate labels use current clip names and readable times', async () => {
  const p = await page(); render(p, fixture);
  p.context.data={status:'clarify',message:'请选择',clarification_id:'c',candidates:[{id:'external-first',label:'video-main / external-first 0—5000 ms'}]};
  p.run('showResponse(data)');
  const button=p.get('messages').querySelectorAll('button').at(-1);
  assert.match(button.textContent,/开场镜头/);
  assert.doesNotMatch(button.textContent,/external-first|video-main|ms/);
});

test('raw parser errors are replaced by a Chinese reason without internal fields', async () => {
  const p = await page();
  p.context.errorText='JSON 格式或字段错误: json: cannot unmarshal array into Go value of type agent.ParseResult';
  const text=p.run('friendlyError(errorText)');
  assert.doesNotMatch(text,/json:|unmarshal|ParseResult/);
  assert.match(text,/未完成|未能/);
  for(const errorText of ['Failed to fetch','network error','The encoded data was not valid for encoding utf-8']){
    p.context.errorText=errorText;
    assert.match(p.run('friendlyError(errorText)'),/传输|连接/);
  }
});

function streamResponse(text) {
  const bytes=new TextEncoder().encode(text);
  return new Response(new ReadableStream({start(controller){for(const byte of bytes)controller.enqueue(Uint8Array.of(byte));controller.close();}}),{headers:{'Content-Type':'text/event-stream'}});
}

test('stream reader handles UTF-8 across packets and ignores identical event IDs', async () => {
  const p=await page();
  p.context.response=streamResponse('id: 1\r\nevent: draft\r\ndata: {"text":"你好😀"}\r\n\r\nid: 1\nevent: draft\ndata: {"text":"你好😀"}\n\nid: 2\nevent: result\ndata: {"status":"answer","message":"你好😀"}\n\n');
  p.run('globalThis.drafts=[]');
  const result=await p.run('readReplyStream(response,(event,data)=>{if(event==="draft")drafts.push(data.text)})');
  assert.equal(result.message,'你好😀');
  assert.equal(p.run('drafts.length'),1);
});

test('unfinished, failed and contradictory streams never yield a confirmable reply', async () => {
  for(const text of [
    'id: 1\nevent: draft\ndata: {"text":"生成一半"}\n\n',
    'id: 1\nevent: error\ndata: {"message":"传输中断"}\n\n',
    'id: 1\nevent: result\ndata: {"status":"plan","preview":{}}\n\nid: 2\nevent: result\ndata: {"status":"plan","preview":{}}\n\n',
    'id: 1\nevent: draft\ndata: {broken}\n\n',
  ]) {
    const p=await page();p.context.response=streamResponse(text);
    await assert.rejects(p.run('readReplyStream(response,()=>{})'));
    assert.equal(p.get('messages').querySelectorAll('button').length,0);
  }
});
