"use strict";
(()=>{
 const control=document.getElementById('languageSelect');
 const current=document.documentElement.lang==='en'?'en':'ko';
 const allowed=lang=>lang==='ko'||lang==='en';
 function confirmChange(lang){
  if(!allowed(lang))return false;
  const dirty=[...document.querySelectorAll('input[type=password]')].some(x=>x.value);
  return lang===current||!dirty||confirm(current==='ko'?'언어를 바꾸면 저장하지 않은 입력은 사라져요. 계속할까요?':'Changing language discards unsaved input. Continue?');
 }
 function save(lang){
  if(!allowed(lang))return false;
  document.cookie='DantamiLanguage='+lang+'; Path=/; Max-Age=31536000; SameSite=Lax'+(location.protocol==='https:'?'; Secure':'');
  return document.cookie.split(';').some(x=>x.trim()==='DantamiLanguage='+lang);
 }
 window.DantamiLanguage={current,confirmChange,save};
 if(!control)return;
 control.value=current;
 if(control.dataset.deferred==='true')return;
 control.addEventListener('change',()=>{
  if(!confirmChange(control.value)||!save(control.value)){control.value=current;return}
  location.reload();
 });
})();
