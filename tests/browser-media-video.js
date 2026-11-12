async page => {
 const base=new URL(page.url()).origin,checks=[];
 const assert=(v,s)=>{if(!v)throw new Error(s);checks.push(s);};
 const sample=await(await page.request.get(base+'/api/sample')).json();
 sample.clips.find(c=>c.id==='clip-a').duration_ms=1000;
 sample.clips.find(c=>c.id==='clip-a').source_in_ms=400;
 await page.request.post(base+'/api/import',{data:sample});
 await page.evaluate(()=>{dirty=false;});await page.reload();await page.waitForFunction(()=>!document.getElementById('send').disabled);
 // Generate a disposable, original local WebM for actual file/decoder verification.
 const bytes=await page.evaluate(async()=>{
  const canvas=document.createElement('canvas');canvas.width=160;canvas.height=90;
  const ctx=canvas.getContext('2d');let n=0;
  const paint=()=>{ctx.fillStyle=n++%2?'#287f70':'#abbf95';ctx.fillRect(0,0,160,90);ctx.fillStyle='white';ctx.fillText(String(n),20,40);};paint();
  const stream=canvas.captureStream(12),recorder=new MediaRecorder(stream),parts=[];
  recorder.ondataavailable=e=>parts.push(e.data);const ended=new Promise(resolve=>recorder.onstop=resolve);
  const timer=setInterval(paint,80);recorder.start();await new Promise(resolve=>setTimeout(resolve,2200));recorder.stop();await ended;clearInterval(timer);for(const track of stream.getTracks())track.stop();
  return Array.from(new Uint8Array(await new Blob(parts,{type:recorder.mimeType}).arrayBuffer()));
 });
 await page.locator('[data-clip-id="clip-a"]').click();
 await page.locator('#media-file').setInputFiles({name:'original-test.webm',mimeType:'video/webm',buffer:Buffer.from(bytes)});
 await page.waitForFunction(()=>document.getElementById('media-video').readyState>=1);
 assert(await page.locator('#media-video').isVisible(),'真实本地视频可解码并显示');
 assert(await page.locator('#media-video').evaluate(v=>Math.abs(v.currentTime-.4)<.06),'视频从素材入点400毫秒开始');
 await page.locator('#media-cursor').evaluate(el=>{el.value='500';el.dispatchEvent(new Event('input'));});
 assert(await page.locator('#media-video').evaluate(v=>Math.abs(v.currentTime-.9)<.06),'时间轴500毫秒映射到源900毫秒');
 await page.locator('#media-video').evaluate(v=>v.play());
 await page.waitForFunction(()=>document.getElementById('media-video').paused&&Number(document.getElementById('media-cursor').value)===1000,{timeout:7000});
 assert(await page.locator('#media-video').isHidden(),'播放到右端停止，不播放裁剪区间之外');
 const exported=await(await page.request.get(base+'/api/export')).json();assert(!JSON.stringify(exported).includes('blob:'),'媒体绑定不写入导出工程');
 return {passed:checks.length,checks};
}
