'use strict';
const $ = id => document.getElementById(id);
let project=null,busy=false,dirty=false,modelAvailable=false,selectedClipID=null,selectedProjectID=null;
let generationController=null;
let auditState={entries:[],can_undo:false,can_redo:false};
let activeDifference=null;
const mediaBindings=new Map();
let mediaTime=0,mediaClipID=null;
const pendingCards=new Set();
const make=(tag,className,text)=>{const e=document.createElement(tag);if(className)e.className=className;if(text!==undefined)e.textContent=text;return e;};
const seconds=ms=>{const whole=Math.floor(ms/1000),fraction=String(ms%1000).padStart(3,'0').replace(/0+$/,'');return `${Math.floor(whole/60)}:${String(whole%60).padStart(2,'0')}${fraction?'.'+fraction:''}`;};
const durationText=ms=>`${Number((ms/1000).toFixed(3))} 秒`;
const orderedClips=(p,track)=>p.clips.filter(c=>c.track_id===track.id).sort((a,b)=>a.start_ms-b.start_ms||a.id.localeCompare(b.id));
function clipName(c,p=project){
 if(c.display_name&&c.display_name.trim())return c.display_name;
 const track=p?.tracks.find(t=>t.id===c.track_id),clips=track?orderedClips(p,track):[c],index=clips.findIndex(item=>item.id===c.id)+1;
 return c.text!==undefined?`第 ${index} 条字幕`:`第 ${index} 段`;
}
function renderExamples(p){
 const suggestions=$('suggestions'),usage=$('usage-examples');suggestions.replaceChildren();usage.replaceChildren();
 $('instruction').placeholder='说说你的修改想法…';
 if(!p){usage.append(make('p','','载入工程后，这里会显示适用于当前片段的示例。'));return;}
 const available=p.tracks.filter(t=>!t.locked).flatMap(t=>orderedClips(p,t)).filter(c=>/^[A-Za-z0-9][A-Za-z0-9_.:-]*$/.test(c.id));
 const video=available.find(c=>c.text===undefined&&c.duration_ms>1),caption=available.find(c=>c.text!==undefined);
 const examples=[];
 if(video){const amount=video.duration_ms>1000?'1 秒':'1 毫秒';examples.push({target:video.id,label:`缩短${clipName(video,p)}`,text:`把这段缩短 ${amount}`});}
 if(caption)examples.push({target:caption.id,label:`修改${clipName(caption,p)}`,text:'把这段的字幕改为“下期见”'});
 if(available.length){const target=available.find(c=>c!==video&&c!==caption)||available[0];examples.push({target:target.id,label:`删除${clipName(target,p)}`,text:'删除这段'});}
 for(const example of examples){const button=make('button','',example.label);button.type='button';button.dataset.example=example.text;button.addEventListener('click',()=>{selectClip(example.target);$('instruction').value=example.text;$('instruction').focus();});suggestions.append(button);usage.append(make('p','',`${example.label}：选中对应片段后，输入“${example.text}”`));}
 if(examples.length)$('instruction').placeholder='说说你的修改想法…\n点击上方示例可填入当前片段的指令';
 else usage.append(make('p','','当前没有适用的离线示例。锁定片段或含特殊符号的标识不会生成示例指令。'));
}
function updateSelection(){
 const clip=project?.clips.find(c=>c.id===selectedClipID);
 $('selected-context').textContent=clip?'当前选中：'+clipName(clip):'未选择片段；点击时间轴里的镜头或字幕';
 for(const element of document.querySelectorAll('[data-clip-id]'))element.classList.toggle('selected',element.dataset.clipId===selectedClipID);
}
function selectClip(id){
 if(!project?.clips.some(c=>c.id===id))return;
 selectedClipID=id;selectedProjectID=project.project_id;updateSelection();updateMedia(true);
}
function toast(text){$('toast').textContent=text;$('toast').hidden=false;clearTimeout(toast.timer);toast.timer=setTimeout(()=>$('toast').hidden=true,5000);}
const statusLabels={answer:'咨询回复',clarify:'需要补充信息',plan:'待确认',executed:'已执行',error:'本次未完成',unsupported:'暂时不支持',generating:'生成中 · 尚未完成'};
function message(text,role='assistant',status=role==='error'?'error':'answer'){
 const e=make('article',`message ${role}`);e.dataset.status=status;
 e.append(make('span','message-label',role==='user'?'你':`剪辑助手 · ${statusLabels[status]||'说明'}`),make('p','',text));
 $('messages').append(e);$('messages').scrollTop=$('messages').scrollHeight;return e;
}
function friendlyError(text){
 if(/failed to fetch|network|encoded data|load failed|connection|terminated/i.test(String(text)))return '连接或回复传输中断，本次未完成；工程没有因此改变。请确认程序仍在运行后重试。';
 if(/json:|unmarshal|ParseResult|JSON 格式|SyntaxError|Unexpected token|unknown field/i.test(String(text)))return '本次未能读懂回复或工程文件，工程没有因此改变。请重试；导入文件时请检查文件格式。';
 return String(text||'本次未完成，请稍后重试。');
}
function trackName(id,p=project){const track=p?.tracks.find(t=>t.id===id);if(!track)return '目标轨道';const tracks=p.tracks.filter(t=>t.kind===track.kind);return `${track.kind==='caption'?'字幕':'视频'}轨道 ${tracks.indexOf(track)+1}`;}
function candidateLabel(candidate){const clip=project?.clips.find(c=>c.id===candidate.id);return clip?`${clipName(clip)} · ${trackName(clip.track_id)} · ${seconds(clip.start_ms)}—${seconds(clip.start_ms+clip.duration_ms)}`:'候选片段';}
function updateControls(){for(const id of ['undo','redo','export','insert-preview','media-file','demo-cover'])$(id).disabled=busy||!project;$('undo').disabled=busy||!project||!auditState.can_undo;$('redo').disabled=busy||!project||!auditState.can_redo;$('check-model').disabled=busy||!modelAvailable;$('send').disabled=busy||(!project&&!($('mode').value==='model'&&modelAvailable));$('sample').disabled=busy;$('import-file').disabled=busy;$('mode').disabled=busy;$('send').textContent=busy?'处理中…':'发送 ↑';$('save-note').classList.toggle('dirty',dirty);$('save-note').textContent=dirty?'当前工程有尚未导出的状态。点击“导出工程”下载；程序重启后无法恢复未导出的修改及撤销历史。':'点击导出才能保留工程文件；程序重启后，不保留未导出的修改和撤销历史。';}
async function api(path,body){const res=await fetch(`/api/${path}`,{method:body===undefined?'GET':'POST',credentials:'same-origin',headers:body===undefined?{}:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});let data;try{data=await res.json();}catch{throw new Error('服务返回异常，请检查程序是否运行。');}if(!res.ok)throw new Error(data.error||`请求失败 (${res.status})`);return data;}
async function action(fn){if(busy)return;busy=true;updateControls();try{await fn();}catch(err){const text=friendlyError(err.message);message(text,'error');toast(text);}finally{busy=false;updateControls();}}
function invalidateCards(){for(const card of pendingCards){for(const b of card.querySelectorAll('button'))b.disabled=true;card.append(make('div','preview-meta','该预览已失效或已处理'));}pendingCards.clear();clearDifference();}
function render(p){if(!p||selectedProjectID!==p.project_id||!p.clips.some(c=>c.id===selectedClipID))selectedClipID=null;selectedProjectID=p?.project_id??null;project=p;renderExamples(p);updateSelection();$('empty').hidden=!!p;$('timeline-panel').hidden=!p;$('details-panel').hidden=!p;if(!p){updateControls();return;}$('project-title').textContent='当前时间轴工程';$('project-meta').textContent=`版本 ${p.revision} · ${p.tracks.length} 条轨道 · ${p.clips.length} 个片段`;$('clip-count').textContent=`${p.clips.length} 个片段`;$('timeline').replaceChildren();$('clip-rows').replaceChildren();const end=Math.max(10000,...p.clips.map(c=>c.start_ms+c.duration_ms))*1.08;const ruler=make('div','ruler');ruler.append(make('div','lane-name','时间 / 分:秒'));const marks=make('div','ruler-track');for(let i=0;i<=5;i++)marks.append(make('span','',seconds(Math.round(end*i/5))));ruler.append(marks);$('timeline').append(ruler);
 for(const track of p.tracks){const lane=make('div','lane'),name=make('div','lane-name');name.append(make('strong','',trackName(track.id)),make('small','',track.locked?'🔒 已锁定':track.kind==='video'?'视频轨道':'字幕轨道'));const rail=make('div','lane-track');for(const c of orderedClips(p,track)){const labelText=clipName(c,p),bar=make('div',`clip ${track.kind==='caption'?'caption':''} ${track.locked?'locked':''}`);bar.style.left=`${c.start_ms/end*100}%`;bar.style.width=`${c.duration_ms/end*100}%`;bar.title=`${labelText} · ${seconds(c.start_ms)}—${seconds(c.start_ms+c.duration_ms)}${c.text!==undefined?' · '+c.text:''}`;bar.append(make('strong','',labelText),make('small','',c.text??durationText(c.duration_ms)));bar.dataset.clipId=c.id;bar.role='button';bar.tabIndex=0;bar.addEventListener('click',()=>selectClip(c.id));bar.addEventListener('keydown',event=>{if(event.key==='Enter'||event.key===' '){event.preventDefault();selectClip(c.id);}});rail.append(bar);const row=make('tr');const label=make('td','',labelText);if(c.text!==undefined)label.append(make('small','',c.text));else label.append(make('small','',`素材入点 ${seconds(c.source_in_ms)} / 源时长 ${durationText(c.source_duration_ms)}`));row.append(label,make('td','',trackName(track.id)+(track.locked?' 🔒':'')),make('td','',`${seconds(c.start_ms)}—${seconds(c.start_ms+c.duration_ms)}`),make('td','',durationText(c.duration_ms)));$('clip-rows').append(row);}lane.append(name,rail);$('timeline').append(lane);}updateSelection();updateControls();}
function preview(data){
 const p=data.preview,e=message(data.message,'assistant','plan'),card=make('section','preview-card');renderDifference(p);
 card.append(make('h3','','待确认的修改'),make('div','preview-meta',`基于版本 ${p.revision} · ${p.operations.length} 项操作`));
 let hasDelete=false,hasVideo=false;
 for(const change of p.changes){
  const block=make('div','change'),before=change.before,after=change.after;
  if((after||before).text===undefined)hasVideo=true;
  block.append(make('strong','',clipName(after||before)));
  if(!before){block.append(make('div','',`将插入新副本 · ${trackName(after.track_id)} · ${seconds(after.start_ms)}—${seconds(after.start_ms+after.duration_ms)}`));}
  else if(!after){block.append(make('div','danger-note',`将删除此片段（${seconds(before.start_ms)}—${seconds(before.start_ms+before.duration_ms)}）`));hasDelete=true;}
  else{
   for(const [key,label] of [['track_id','所在轨道'],['start_ms','起点'],['duration_ms','时长'],['text','字幕文字']]){
    if(before?.[key]!==after[key]){
     const format=v=>v===undefined?'—':key==='start_ms'?seconds(v):key==='duration_ms'?durationText(v):key==='track_id'?trackName(v):String(v);
     const line=make('div','change-line',`${label}将从 ${format(before?.[key])} 改为 `);line.append(make('span','',format(after[key])));block.append(line);
    }
   }
   block.append(make('div','preview-meta',`修改后时间：${seconds(after.start_ms)}—${seconds(after.start_ms+after.duration_ms)}`));
  }
  card.append(block);
 }
 if(hasVideo)card.append(make('p','preview-note','字幕不会自动跟随视频移动、缩短或删除；只有本预览明确列出的字幕修改才会执行。'));
 if(!p.changes.length)card.append(make('p','preview-note','与当前工程相比没有实际变化。'));
 if(hasDelete)card.append(make('p','danger-note','包含删除操作。确认后移除，可在本次运行中撤销。'));
 const buttons=make('div','preview-actions'),cancel=make('button','button','取消'),confirm=make('button','button primary','确认修改');cancel.type=confirm.type='button';buttons.append(cancel,confirm);card.append(buttons);e.append(card);pendingCards.add(card);
 const compare=make('button','button','查看时间轴差异');compare.type='button';compare.addEventListener('click',()=>{renderDifference(p);$('difference-panel').scrollIntoView?.({behavior:'smooth',block:'nearest'});});buttons.prepend?.(compare);
 confirm.addEventListener('click',()=>action(async()=>{const r=await api('confirm',{plan_id:p.id});for(const op of p.operations)if(op.type==='insert'&&mediaBindings.has(op.source_id))mediaBindings.set(op.target_id,mediaBindings.get(op.source_id));invalidateCards();dirty=true;render(r.project);await refreshHistory();showActualResult(r);toast('修改已应用');}));
 cancel.addEventListener('click',()=>action(async()=>{const r=await api('cancel',{plan_id:p.id});confirm.disabled=cancel.disabled=true;pendingCards.delete(card);compare.disabled=true;if(activeDifference===p.id)clearDifference();card.append(make('div','preview-meta','已取消'));message(r.message);}));$('messages').scrollTop=$('messages').scrollHeight;
}
function showResponse(data){
 if(data.status==='plan'){preview(data);return;}
 const e=message(data.message,'assistant',data.status);
 if(data.status==='clarify')for(const c of data.candidates||[]){const label=candidateLabel(c),b=make('button','candidate',label);b.type='button';b.addEventListener('click',()=>action(async()=>{b.disabled=true;await send({choice:c.id,clarification_id:data.clarification_id},`选择 ${label}`);}));e.append(b);}
}
async function readReplyStream(response,onEvent){
 if(!response.ok||!response.headers.get('Content-Type')?.includes('text/event-stream')){
  let data;try{data=await response.json();}catch{throw new Error('服务返回异常，请检查程序是否运行。');}
  throw new Error(data.error||'当前服务无法提供流式回复，请重试。');
 }
 const reader=response.body.getReader(),decoder=new TextDecoder('utf-8',{fatal:true});
 let buffer='',total=0,lastID=0,lastFrame='',result=null;
 const frame=raw=>{
  let event='',id='',lines=[];
  for(const line of raw.split(/\r?\n/)){
   if(line.startsWith('event:'))event=line.slice(6).trim();
   else if(line.startsWith('id:'))id=line.slice(3).trim();
   else if(line.startsWith('data:'))lines.push(line.slice(5).replace(/^ /,''));
  }
  if(!lines.length)return;
  const n=Number(id),signature=event+'\n'+lines.join('\n');
  if(n===lastID&&signature===lastFrame)return;
  if(!Number.isSafeInteger(n)||n!==lastID+1||result)throw new Error('回复传输顺序异常，本次未完成，请重试。');
  lastID=n;lastFrame=signature;
  const data=JSON.parse(lines.join('\n'));
  if(event==='error')throw new Error(data.message||'回复未完成，请重试。');
  if(event==='result'){if(!['answer','clarify','unsupported','plan'].includes(data.status))throw new Error('回复未完成，请重试。');result=data;}
  else if(['progress','draft','reset'].includes(event))onEvent(event,data);
  else throw new Error('回复传输格式异常，本次未完成，请重试。');
 };
 try{
  while(true){const {value,done}=await reader.read();if(done)break;total+=value.byteLength;if(total>4*1024*1024)throw new Error('回复过长，本次未完成。请缩短问题后重试。');buffer+=decoder.decode(value,{stream:true});let match;while((match=/\r?\n\r?\n/.exec(buffer))){frame(buffer.slice(0,match.index));buffer=buffer.slice(match.index+match[0].length);}}
  buffer+=decoder.decode();
  if(buffer.trim()||!result)throw new Error('回复传输中断，本次未完成；工程没有因此改变，请重试。');
  return result;
 }finally{await reader.cancel().catch(()=>{});reader.releaseLock();}
}
async function requestStream(payload){
 const article=message('正在连接助手…','assistant','generating'),text=article.children[1];let draft='';
 generationController=new AbortController();$('stop-generation').hidden=false;
 try{
  const response=await fetch('/api/message/stream',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload),signal:generationController.signal});
  return await readReplyStream(response,(event,data)=>{
   if(event==='reset'){draft='';text.textContent=data.message;}
   else if(event==='draft'){draft+=data.text;text.textContent=draft;}
   else if(!draft)text.textContent=data.message;
   $('messages').scrollTop=$('messages').scrollHeight;
  });
 }catch(err){if(generationController.signal.aborted)throw new Error('已停止生成，本次没有应用修改。');throw err;}
 finally{article.remove();generationController=null;$('stop-generation').hidden=true;}
}
async function send(payload,label){message(label,'user');const input={...payload,mode:$('mode').value,...(selectedClipID?{selected_clip_id:selectedClipID}:{})};const data=input.mode==='model'?await requestStream(input):await api('message',input);showResponse(data);}
async function importProject(data){if(project&&!window.confirm('导入会替换当前工程，并清空待确认计划和撤销历史。未导出的状态将丢失，是否继续？'))return;const result=await api('import',data);invalidateCards();clearMediaBindings();$('messages').replaceChildren();dirty=false;selectedClipID=null;selectedProjectID=null;render(result.project);await refreshHistory();message(result.message+'。现在可以输入修改要求。');}
$('sample').addEventListener('click',()=>action(async()=>{await importProject(await api('sample'));}));
$('import-file').addEventListener('change',()=>{const file=$('import-file').files[0];if(!file)return;action(async()=>{try{if(file.size>1024*1024)throw new Error('工程文件不能超过 1 MiB');await importProject(JSON.parse(await file.text()));}finally{$('import-file').value='';}});});
$('message-form').addEventListener('submit',event=>{event.preventDefault();const text=$('instruction').value.trim();if(!text)return;action(async()=>{$('instruction').value='';await send({text},text);});});
$('instruction').addEventListener('keydown',e=>{if(e.key==='Enter'&&!e.shiftKey&&!e.isComposing){e.preventDefault();$('message-form').requestSubmit();}});
$('stop-generation').addEventListener('click',()=>generationController?.abort());
const roleHelp=$('role-help'),roleToggle=$('role-toggle');
roleToggle.addEventListener('click',()=>{const open=roleHelp.dataset.open!=='true';roleHelp.dataset.open=String(open);roleHelp.dataset.dismissed=String(!open);roleToggle.setAttribute('aria-expanded',String(open));});
roleHelp.addEventListener('mouseenter',()=>{roleHelp.dataset.dismissed='false';});
roleToggle.addEventListener('focus',()=>{roleHelp.dataset.dismissed='false';});
roleHelp.addEventListener('keydown',event=>{if(event.key==='Escape'){roleHelp.dataset.open='false';roleHelp.dataset.dismissed='true';roleToggle.setAttribute('aria-expanded','false');roleToggle.blur();}});
$('mode').addEventListener('change',()=>{$('mode-help').textContent=$('mode').value==='model'?'剪辑咨询可直接提问，编辑前会先澄清或预览':'离线模式支持明确句式';updateControls();});
for(const route of ['undo','redo'])$(route).addEventListener('click',()=>action(async()=>{const request_id=crypto.randomUUID();const r=await api(route,{request_id});invalidateCards();dirty=true;render(r.project);await refreshHistory();showActualResult(r);toast(route==='undo'?'已撤销':'已重做');}));
$('export').addEventListener('click',()=>action(async()=>{const data=await api('export');const blob=new Blob([JSON.stringify(data,null,2)+'\n'],{type:'application/json'}),url=URL.createObjectURL(blob),a=make('a');a.href=url;a.download=`${data.project_id.replace(/[^\w\-\u4e00-\u9fff]/g,'_')}-r${data.revision}.json`;document.body.append(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(url),10000);dirty=false;toast('已发起工程下载，请在浏览器中确认文件');}));
window.addEventListener('beforeunload',event=>{if(dirty){event.preventDefault();event.returnValue='';}});
function changeDescription(change){
 const b=change.before,a=change.after,c=a||b;
 const name=c.display_name||((project?.clips.some(item=>item.id===c.id))?clipName(c):c.text!==undefined?'字幕':'片段');
 if(!b)return `插入${name} · ${trackName(a.track_id)} · ${seconds(a.start_ms)}—${seconds(a.start_ms+a.duration_ms)}`;
 if(!a)return `删除${name} · ${seconds(b.start_ms)}—${seconds(b.start_ms+b.duration_ms)}`;
 const fields=[];
 if(b.track_id!==a.track_id)fields.push(`${trackName(b.track_id)} → ${trackName(a.track_id)}`);
 if(b.start_ms!==a.start_ms)fields.push(`起点 ${seconds(b.start_ms)} → ${seconds(a.start_ms)}`);
 if(b.duration_ms!==a.duration_ms)fields.push(`时长 ${durationText(b.duration_ms)} → ${durationText(a.duration_ms)}`);
 if(b.text!==a.text)fields.push(`字幕“${b.text}” → “${a.text}”`);
 return `${name}：${fields.join('；')}`;
}
function showActualResult(result){
 const e=message(`${result.message}。当前版本 ${result.project.revision}。`,'assistant','executed');
 for(const change of result.changes)e.append(make('p','actual-change',changeDescription(change)));
}
function clearDifference(){activeDifference=null;$('difference-panel').hidden=true;$('difference-content').replaceChildren();}
function renderDifference(plan){
 activeDifference=plan.id;$('difference-panel').hidden=false;const container=$('difference-content');container.replaceChildren();
 const clips=plan.changes.flatMap(c=>[c.before,c.after].filter(Boolean));
 const end=Math.max(1000,...clips.map(c=>c.start_ms+c.duration_ms))*1.05;
 for(const change of plan.changes){
  const block=make('div','difference-item'),target=change.after||change.before;
  const title=make('button','text-button',target.display_name||clipName(target));title.type='button';title.addEventListener('click',()=>selectClip(target.id));block.append(title);
  for(const [clip,label,kind] of [[change.before,'修改前','before'],[change.after,'修改后','after']]){
   const row=make('div','difference-row');row.append(make('span','',label));const rail=make('div','difference-rail');
   if(clip){const bar=make('div','difference-bar '+kind,`${seconds(clip.start_ms)}—${seconds(clip.start_ms+clip.duration_ms)}`);bar.style.left=`${clip.start_ms/end*100}%`;bar.style.width=`${clip.duration_ms/end*100}%`;bar.title=`${trackName(clip.track_id)} · ${bar.textContent}`;rail.append(bar);}
   else rail.append(make('span','muted',kind==='before'?'尚无此片段':'将删除'));
   row.append(rail);block.append(row);
  }
  block.append(make('p','muted',changeDescription(change)));container.append(block);
 }
}
function renderHistory(data){
 auditState={entries:[],can_undo:false,can_redo:false,...data};const list=$('history-list');list.replaceChildren();
 const entries=auditState.entries||[];
 if(!entries.length)list.append(make('p','muted','还没有已提交的修改。预览、取消和失败不会记为执行成功。'));
 for(const entry of [...entries].reverse()){
  const item=make('details','history-entry'),label={edit:'已执行',undo:'已撤销',redo:'已重做'}[entry.action]||'记录';
  item.append(make('summary','',`${label} · 版本 ${entry.revision} · ${entry.changes.length} 个片段`));
  item.append(make('p','muted',new Date(entry.time).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai'})));
  for(const change of entry.changes)item.append(make('p','',changeDescription(change)));
  list.append(item);
 }
 $('undo').title=auditState.can_undo?'撤销：'+(auditState.undo_summary||'上一条完整指令'):'没有可撤销的修改';
 $('redo').title=auditState.can_redo?'重做：'+(auditState.redo_summary||'上一条完整指令'):'没有可重做的修改';updateControls();
}
async function refreshHistory(){
 try{renderHistory(await api('history'));}
 catch(err){auditState={entries:[],can_undo:false,can_redo:false};updateControls();$('history-list').textContent='记录暂时无法读取，请刷新后重试。当前工程不会因此回滚。';}
}
function renderInsertOptions(){
 const sources=$('insert-source'),tracks=$('insert-track'),previous=sources.value;sources.replaceChildren();tracks.replaceChildren();
 if(!project)return;
 for(const track of project.tracks)for(const clip of orderedClips(project,track)){const o=make('option','',`${clipName(clip)} · ${trackName(clip.track_id)}`);o.value=clip.id;sources.append(o);}
 if(project.clips.some(c=>c.id===previous))sources.value=previous;
 renderInsertTracks();
}
function renderInsertTracks(){
 const tracks=$('insert-track'),previous=tracks.value;tracks.replaceChildren();
 const source=project?.clips.find(c=>c.id===$('insert-source').value)||project?.clips[0];if(!source)return;
 const kind=project.tracks.find(t=>t.id===source.track_id).kind;
 for(const track of project.tracks.filter(t=>t.kind===kind&&!t.locked)){const o=make('option','',trackName(track.id));o.value=track.id;tracks.append(o);}
 if(project.tracks.some(t=>t.id===previous&&t.kind===kind&&!t.locked))tracks.value=previous;
}
function previewFrame(clip,time,p){
 const visible=time>=clip.start_ms&&time<clip.start_ms+clip.duration_ms;
 return {visible,source_ms:clip.source_in_ms+Math.max(0,Math.min(clip.duration_ms,time-clip.start_ms)),captions:p.clips.filter(c=>c.text!==undefined&&time>=c.start_ms&&time<c.start_ms+c.duration_ms)};
}
function currentMediaClip(){return project?.clips.find(c=>c.id===selectedClipID&&c.text===undefined)||project?.clips.find(c=>c.id===mediaClipID&&c.text===undefined)||project?.tracks.flatMap(t=>orderedClips(project,t)).find(c=>c.text===undefined);}
function updateMedia(seek=false){
 const clip=currentMediaClip(),video=$('media-video'),image=$('media-image');
 if(!clip){video.pause?.();video.hidden=image.hidden=true;$('media-empty').hidden=false;$('media-title').textContent='先选择一个视频片段';return;}
 const changed=mediaClipID!==clip.id;mediaClipID=clip.id;if(seek||changed)mediaTime=clip.start_ms;
 const binding=mediaBindings.get(clip.id),frame=previewFrame(clip,mediaTime,project);
 $('media-title').textContent=`${clipName(clip)} · ${seconds(clip.start_ms)}—${seconds(clip.start_ms+clip.duration_ms)} · ${binding?binding.name:'尚未绑定媒体'}`;
 $('media-cursor').max=String(Math.max(1000,...project.clips.map(c=>c.start_ms+c.duration_ms)));$('media-cursor').value=String(mediaTime);$('media-position').textContent=seconds(mediaTime);
 $('media-caption').textContent=frame.captions.map(c=>c.text).join('\n');
 video.hidden=!binding||binding.kind!=='video'||!frame.visible;image.hidden=!binding||binding.kind!=='image'||!frame.visible;
 $('media-empty').hidden=!!binding&&frame.visible;
 $('media-empty').textContent=!frame.visible?'当前时间不在所选片段内。拖动时间滑块可查看该片段。':'为此片段绑定本地视频或图片；也可使用演示封面。';
 if(binding){
  const element=binding.kind==='video'?video:image;
  if(element.dataset.binding!==binding.url){video.pause?.();element.src=binding.url;element.dataset.binding=binding.url;}
  if(binding.kind==='video'){
   if(!frame.visible)video.pause?.();
   else if((seek||changed||video.paused)&&video.readyState>=1&&Math.abs(video.currentTime-frame.source_ms/1000)>.05)video.currentTime=frame.source_ms/1000;
  }else video.pause?.();
 }else video.pause?.();
}
function clearMediaBindings(){
 $('media-video').pause?.();for(const url of new Set([...mediaBindings.values()].filter(b=>b.local).map(b=>b.url)))URL.revokeObjectURL(url);
 mediaBindings.clear();mediaClipID=null;mediaTime=0;$('media-video').removeAttribute?.('src');$('media-image').removeAttribute?.('src');$('media-video').dataset.binding='';$('media-image').dataset.binding='';
}
function bindMedia(file){
 const clip=currentMediaClip();if(!clip)throw new Error('请先选择视频片段。');
 if(!/^video\//.test(file.type)&&!/^image\/(png|jpeg|webp|gif)$/.test(file.type))throw new Error('请选择浏览器支持的视频或 PNG、JPEG、WebP、GIF 图片。');
 if(file.size>512*1024*1024)throw new Error('预览文件不能超过 512 MiB。');
 mediaBindings.set(clip.id,{url:URL.createObjectURL(file),name:file.name,kind:file.type.startsWith('video/')?'video':'image',local:true});updateMedia(true);
}
const renderTimeline=render;
render=function(p){renderTimeline(p);for(const id of ['insert-panel','media-panel','history-panel'])$(id).hidden=!p;renderInsertOptions();updateMedia();};
$('insert-source').addEventListener('change',renderInsertTracks);
$('insert-form').addEventListener('submit',event=>{event.preventDefault();action(async()=>{
 const source=project?.clips.find(c=>c.id===$('insert-source').value),track=$('insert-track').value,secondsValue=$('insert-time').value;
 if(!source||!track||!/^\d+(?:\.\d{1,3})?$/.test(secondsValue))throw new Error('请选择来源和目标轨道，起点最多三位小数。');
 const start=Number(secondsValue)*1000;if(!Number.isSafeInteger(Math.round(start))||start<0)throw new Error('时间超出可用范围。');
 const data=await api('insert-preview',{source_id:source.id,track_id:track,start_ms:Math.round(start)});showResponse(data);
});});
$('media-file').addEventListener('change',()=>{const file=$('media-file').files?.[0];if(!file)return;try{bindMedia(file);}catch(err){toast(err.message);}finally{$('media-file').value='';}});
$('demo-cover').addEventListener('click',()=>{const clip=currentMediaClip();if(!clip)return;mediaBindings.set(clip.id,{url:'/demo-cover.svg',name:'虚构演示封面',kind:'image',local:false});updateMedia(true);});
$('media-cursor').addEventListener('input',()=>{mediaTime=Number($('media-cursor').value);$('media-video').pause?.();updateMedia(false);const clip=currentMediaClip();if(clip){const frame=previewFrame(clip,mediaTime,project);if(frame.visible&&$('media-video').readyState>=1)$('media-video').currentTime=frame.source_ms/1000;}});
$('media-video').addEventListener('loadedmetadata',()=>{const clip=currentMediaClip();if(!clip)return;const v=$('media-video');v.currentTime=clip.source_in_ms/1000;if(v.duration*1000<clip.source_in_ms+clip.duration_ms)$('media-note').textContent='绑定视频短于工程声明的素材区间，超出部分无法播放；工程数据没有改变。';});
$('media-video').addEventListener('timeupdate',()=>{const clip=currentMediaClip(),v=$('media-video');if(!clip||v.hidden)return;mediaTime=clip.start_ms+Math.round(v.currentTime*1000)-clip.source_in_ms;if(mediaTime>=clip.start_ms+clip.duration_ms){v.pause();mediaTime=clip.start_ms+clip.duration_ms;}else if(mediaTime<clip.start_ms){v.currentTime=clip.source_in_ms/1000;mediaTime=clip.start_ms;}updateMedia();});
$('media-video').addEventListener('error',()=>{if(mediaBindings.get(mediaClipID)?.kind==='video')toast('此视频无法预览，请换用浏览器支持的视频格式；工程没有改变。');});
$('media-image').addEventListener('error',()=>toast('图片无法预览，请重新绑定；工程没有改变。'));
function renderModelStatus(status){
 const labels={unconfigured:'模型 · 未配置',pending:'模型 · 待适配',connected:'模型 · 已连通',unavailable:'模型 · 暂不可用'};
 const option=$('mode').querySelector('[value="model"]');option.textContent=labels[status.state]||'模型 · 待适配';option.disabled=!modelAvailable;$('model-status').textContent=status.message||'在线模型可选，离线功能不依赖密钥。';$('check-model').disabled=!modelAvailable||busy;
}
$('check-model').addEventListener('click',()=>action(async()=>{$('model-status').textContent='正在检查普通回复与流式协议，不会修改工程…';renderModelStatus(await api('model/check',{}));}));
(async()=>{try{const session=await api('session',{});modelAvailable=session.model_available;const option=$('mode').querySelector('[value="model"]');option.disabled=!modelAvailable;option.textContent=modelAvailable?'模型 · 自由表达':'模型 · 待配置';$('mode').value='offline';$('mode-help').textContent='离线模式支持明确句式';$('connection').textContent=modelAvailable?'已连接 · 可切换模型进行剪辑咨询':'已连接 · 离线可用';const state=await api('state');dirty=!!state.project;render(state.project);await refreshHistory();renderModelStatus(await api('model/status'));}catch(err){$('connection').textContent='连接失败';message(err.message,'error');}})();
