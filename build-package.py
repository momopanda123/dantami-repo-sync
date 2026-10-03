from pathlib import Path
import tarfile,shutil,json,hashlib,io,os,subprocess
from PIL import Image,ImageDraw
root=Path(__file__).parent
stage=root/'.agents/package'
if stage.exists():shutil.rmtree(stage)
(stage/'payload/bin').mkdir(parents=True)
shutil.copy2(root/'.agents/repo-sync',stage/'payload/bin/repo-sync')
shutil.copytree(root/'ui',stage/'payload/ui',dirs_exist_ok=True)
(stage/'payload/ui/images').mkdir(parents=True,exist_ok=True)
# Only an inert legacy entry point is public. Real UI/assets require DSM auth.
(stage/'payload/ui/index.html').write_text('<!doctype html><html lang="ko"><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=index.cgi"><title>앱 로그인 확인</title><body><a href="index.cgi">앱 로그인 후 열기</a></body></html>',encoding='utf-8')
for f in ('app.js','style.css','dantami-app.css'):
 (stage/'payload/ui'/f).write_text('/* Legacy static asset retired. Use authenticated index.cgi. */\n')
launcher=stage/'payload/ui/index.cgi';launcher.write_text('#!/bin/sh\nexec /var/packages/DantamiRepoSync/target/bin/repo-sync\n');launcher.chmod(0o755)
for sz in [16,24,32,48,64,72,96,256]:
 im=Image.new('RGBA',(256,256),(0,0,0,0));d=ImageDraw.Draw(im);d.rounded_rectangle((6,6,250,250),radius=52,fill='#16483c');d.polygon([(48,83),(166,83),(166,56),(214,105),(166,150),(166,121),(48,121)],fill='#b0efd1');d.polygon([(208,172),(90,172),(90,199),(42,150),(90,105),(90,134),(208,134)],fill='#f3fff8');im.resize((sz,sz),Image.Resampling.LANCZOS).save(stage/f'payload/ui/images/icon_{sz}.png')
shutil.copytree(root/'WIZARD_UIFILES',stage/'WIZARD_UIFILES');shutil.copytree(root/'scripts',stage/'scripts');shutil.copytree(root/'conf',stage/'conf');shutil.copy2(root/'INFO',stage/'INFO')
shutil.copy2(stage/'payload/ui/images/icon_64.png',stage/'PACKAGE_ICON.PNG');shutil.copy2(stage/'payload/ui/images/icon_256.png',stage/'PACKAGE_ICON_256.PNG')
licenses=stage/'payload/licenses';licenses.mkdir();raw=(root/'.agents/modules.json').read_text();dec=json.JSONDecoder();i=0
while i<len(raw):
 while i<len(raw) and raw[i].isspace():i+=1
 if i==len(raw):break
 obj,j=dec.raw_decode(raw,i);i=j
 if obj.get('Main') or not obj.get('Dir'):continue
 path=Path(obj['Dir']);dest=licenses/obj['Path'].replace('/','_');dest.mkdir()
 for pattern in ['LICENSE*','COPYING*','NOTICE*']:
  for f in path.glob(pattern):
   if f.is_file():shutil.copy2(f,dest/f.name)
shutil.copy2(root/'LICENSE',licenses/'DANTAMI-MIT-LICENSE')
shutil.copy2(Path(subprocess.check_output(['go','env','GOROOT'],text=True).strip())/'LICENSE',licenses/'GO-LICENSE')
(stage/'payload/사용안내.txt').write_text((root/'README.ko.md').read_text(),encoding='utf-8')
(stage/'payload/README_EN.md').write_text((root/'README.md').read_text(),encoding='utf-8')
# Preserve source for audit without requiring user build steps.
src=stage/'payload/source';src.mkdir()
for f in ['i18n.go','i18n_test.go','access_check.go','access_check_test.go','auth.go','auth_test.go','main.go','app.go','discover.go','sync.go','sync_test.go','manager_test.go','nas_connection.go','nas_connection_test.go','diagnostics.go','cgi_test.go','go.mod','go.sum']:shutil.copy2(root/f,src/f)
shutil.copytree(root/'web',src/'web')
with tarfile.open(stage/'package.tgz','w:gz',format=tarfile.USTAR_FORMAT) as t:
 for path in sorted((stage/'payload').iterdir()):t.add(path,arcname=path.name)
info=(stage/'INFO').read_text();info+='checksum="'+hashlib.md5((stage/'package.tgz').read_bytes()).hexdigest()+'"\n';(stage/'INFO').write_text(info)
(root/'dist').mkdir(exist_ok=True)
output=root/'dist/Dantami-Repo-Sync-1.5.0-DSM7-x86_64.spk'
with tarfile.open(output,'w',format=tarfile.USTAR_FORMAT) as t:
 for n in ['INFO','package.tgz','scripts','conf','WIZARD_UIFILES','PACKAGE_ICON.PNG','PACKAGE_ICON_256.PNG']:t.add(stage/n,arcname=n)
with tarfile.open(output) as t:
 assert set(['INFO','package.tgz','conf/privilege','scripts/start-stop-status']).issubset(t.getnames())
 with tarfile.open(fileobj=io.BytesIO(t.extractfile('package.tgz').read()),mode='r:gz') as p:
  assert p.getmember('bin/repo-sync').mode & 0o111
  assert p.getmember('ui/index.cgi').mode & 0o111
  for n in p.getnames():assert '.agents' not in n and '..' not in Path(n).parts
print(output,output.stat().st_size,hashlib.sha256(output.read_bytes()).hexdigest())
