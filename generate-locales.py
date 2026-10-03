"""Generate English UI assets from the reviewed bilingual catalog."""
from pathlib import Path
from html.parser import HTMLParser
import html,json,re
root=Path(__file__).parent
catalog=json.loads((root/'web/i18n.json').read_text())
missing=set()
def tr(s):
 if s in catalog:return catalog[s]
 if re.search('[가-힣]',s) and s!='한국어':missing.add(s)
 return s
class EnglishHTML(HTMLParser):
 def __init__(self):super().__init__(convert_charrefs=False);self.out=[]
 def handle_decl(self,d):self.out.append('<!'+d+'>')
 def handle_starttag(self,t,attrs):
  a=[]
  for k,v in attrs:
   if k=='lang':v='en'
   elif v and k in ['placeholder','title','aria-label']:v=tr(v)
   a.append(k if v is None else k+'="'+html.escape(v,quote=True)+'"')
  self.out.append('<'+t+(' '+' '.join(a) if a else '')+'>')
 def handle_endtag(self,t):self.out.append('</'+t+'>')
 def handle_data(self,d):
  x=d.strip();v=tr(x);self.out.append(d if v==x else d[:len(d)-len(d.lstrip())]+html.escape(v,quote=False)+d[len(d.rstrip()):])
 def handle_entityref(self,n):self.out.append('&'+n+';')
 def handle_charref(self,n):self.out.append('&#'+n+';')
 def handle_comment(self,d):self.out.append('<!--'+d+'-->')
for name in ['index','login']:
 # Normalize named spaces before parsing so navigation labels remain translatable.
 src=(root/f'web/{name}.html').read_text().replace('&nbsp;','\u00a0')
 p=EnglishHTML();p.feed(src);(root/f'web/{name}.en.html').write_text(''.join(p.out))
pattern=re.compile(r'''(['"])((?:\\.|(?!\1).)*?)\1''')
for name in ['app','login']:
 src=(root/f'web/{name}.js').read_text()
 def replace(m):
  raw=m.group(2)
  if re.search('[가-힣]',raw):return json.dumps(tr(raw).replace('\\n','\n'),ensure_ascii=False)
  return m.group(0)
 out=pattern.sub(replace,src).replace("'ko-KR'","'en-US'")
 (root/f'web/{name}.en.js').write_text(out)
if missing:raise SystemExit('Missing translations: '+json.dumps(sorted(missing),ensure_ascii=False))
print('Generated Korean/English UI assets; catalog coverage complete')
