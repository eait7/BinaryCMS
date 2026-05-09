package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ez8/gocms/pkg/plugin"
	hplugin "github.com/hashicorp/go-plugin"
	_ "modernc.org/sqlite"
)

type MenuManagerPlugin struct {
	db *sql.DB
}

func (p *MenuManagerPlugin) initDB() {
	os.MkdirAll("plugins_data", 0777)
	var err error
	p.db, err = sql.Open("sqlite", "plugins_data/menu_manager.db")
	if err != nil {
		return
	}
	p.db.Exec("PRAGMA journal_mode=WAL")
	p.db.Exec(`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`)
}

func (p *MenuManagerPlugin) getSetting(key string) string {
	var val string
	p.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	return val
}

func (p *MenuManagerPlugin) setSetting(key, val string) {
	p.db.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", key, val)
}

func (p *MenuManagerPlugin) PluginName() string { return "Menu Manager" }

func (p *MenuManagerPlugin) HookAdminMenu() []plugin.MenuItem {
	return []plugin.MenuItem{
		{Label: "Menu Manager", URL: "/admin/plugin/menu-manager", Icon: "menu-order"},
	}
}

func (p *MenuManagerPlugin) HookBeforeFrontPageRender(content string) string { return content }
func (p *MenuManagerPlugin) HookDashboardWidget() string                     { return "" }
func (p *MenuManagerPlugin) HookAdminTopRightWidget() string                 { return "" }
func (p *MenuManagerPlugin) HookUserProfileTab(userID int) string            { return "" }
func (p *MenuManagerPlugin) HookUserAccountCard(userID int) string           { return "" }
func (p *MenuManagerPlugin) HookUserRegistered(userID int) string            { return "" }

func (p *MenuManagerPlugin) HookAdminRoute(route string) string {
	routePath := route
	if idx := strings.Index(route, "?"); idx != -1 {
		routePath = route[:idx]
	}

	if routePath == "/admin/plugin/menu-manager" {
		return p.renderDashboard()
	}

	return ""
}

// getMenuDataPath finds the data directory
func getDataDir() string {
	if _, err := os.Stat("/app/data"); err == nil {
		return "/app/data"
	}
	if _, err := os.Stat("data"); err == nil {
		return "data"
	}
	return "."
}

// readCurrentMenus reads backend_menus.json (saved layout) or returns nil
func readCurrentMenus() []plugin.MenuItem {
	dir := getDataDir()
	b, err := os.ReadFile(dir + "/backend_menus.json")
	if err != nil {
		return nil
	}
	var menus []plugin.MenuItem
	json.Unmarshal(b, &menus)
	return menus
}

// readAvailableMenus reads backend_menus_available.json (plugin-registered menus)
func readAvailableMenus() []plugin.MenuItem {
	dir := getDataDir()
	b, err := os.ReadFile(dir + "/backend_menus_available.json")
	if err != nil {
		return nil
	}
	var menus []plugin.MenuItem
	json.Unmarshal(b, &menus)
	return menus
}

// getDefaultMenus returns the core hardcoded menus + plugin menus
func getDefaultMenus() []plugin.MenuItem {
	menus := []plugin.MenuItem{
		{Label: "Dashboard", URL: "/admin", Icon: "home"},
		{Label: "Content", URL: "", Icon: "file-text", Children: []plugin.MenuItem{
			{Label: "Posts", URL: "/admin/posts"},
			{Label: "Pages", URL: "/admin/pages"},
			{Label: "Categories", URL: "/admin/categories"},
			{Label: "Tags", URL: "/admin/tags"},
			{Label: "Comments", URL: "/admin/comments"},
		}},
		{Label: "Design & Media", URL: "", Icon: "palette", Children: []plugin.MenuItem{
			{Label: "Media", URL: "/admin/media"},
			{Label: "Menus", URL: "/admin/menus"},
			{Label: "Appearance", URL: "/admin/themes"},
		}},
		{Label: "System", URL: "", Icon: "settings", Children: []plugin.MenuItem{
			{Label: "Users", URL: "/admin/users"},
			{Label: "Plugins", URL: "/admin/plugins"},
			{Label: "Core Settings", URL: "/admin/settings"},
		}},
	}
	available := readAvailableMenus()
	if available != nil {
		menus = append(menus, available...)
	}
	return menus
}

func (p *MenuManagerPlugin) renderDashboard() string {
	// Get current layout (saved or defaults)
	currentMenus := readCurrentMenus()
	if currentMenus == nil {
		currentMenus = getDefaultMenus()
	}

	menusJSON, _ := json.Marshal(currentMenus)
	defaultsJSON, _ := json.Marshal(getDefaultMenus())

	// Count stats
	totalItems := countItems(currentMenus)
	topLevel := len(currentMenus)

	return fmt.Sprintf(`
<style>
.mm-tree{list-style:none;padding:0;margin:0}
.mm-tree .mm-tree{padding-left:28px;margin-top:4px}
.mm-item{background:var(--tblr-bg-surface);border:1px solid var(--tblr-border-color);border-radius:8px;margin-bottom:6px;position:relative;transition:all .2s}
.mm-item:hover{border-color:var(--tblr-primary);box-shadow:0 2px 8px rgba(0,0,0,.06)}
.mm-item.sortable-ghost{opacity:.3;background:var(--tblr-primary-lt);border-style:dashed}
.mm-item.sortable-drag{box-shadow:0 8px 25px rgba(0,0,0,.15);z-index:100}
.mm-header{display:flex;align-items:center;padding:10px 14px;gap:10px;cursor:grab;user-select:none}
.mm-header:active{cursor:grabbing}
.mm-handle{color:var(--tblr-secondary);flex-shrink:0}
.mm-handle:hover{color:var(--tblr-primary)}
.mm-label{font-weight:600;font-size:14px;flex:1}
.mm-url{color:var(--tblr-secondary);font-size:12px;font-family:monospace}
.mm-nest-bar{position:absolute;left:0;top:0;bottom:0;width:3px;background:var(--tblr-primary);border-radius:3px 0 0 3px;opacity:0}
.mm-tree .mm-tree .mm-item .mm-nest-bar{opacity:1}
.mm-icon-grid{display:grid;grid-template-columns:repeat(6,1fr);gap:6px;max-height:220px;overflow-y:auto}
.mm-icon-btn{border:1px solid var(--tblr-border-color);border-radius:6px;padding:8px;text-align:center;cursor:pointer;transition:all .15s;background:var(--tblr-bg-surface)}
.mm-icon-btn:hover{border-color:var(--tblr-primary);background:var(--tblr-primary-lt)}
.mm-icon-btn.active{border-color:var(--tblr-primary);background:var(--tblr-primary-lt);box-shadow:0 0 0 2px var(--tblr-primary)}
.mm-icon-btn svg{width:20px;height:20px}
.mm-toast{position:fixed;bottom:24px;right:24px;z-index:9999;min-width:300px;opacity:0;transform:translateY(20px);transition:all .3s}
.mm-toast.show{opacity:1;transform:translateY(0)}
.mm-preview{display:flex;gap:4px;flex-wrap:wrap;padding:12px;background:var(--tblr-bg-surface);border:1px solid var(--tblr-border-color);border-radius:8px}
.mm-preview-item{font-size:12px;padding:4px 10px;border-radius:4px;background:var(--tblr-primary-lt);color:var(--tblr-primary);font-weight:600}
</style>

<div class="row row-cards mb-3">
  <div class="col-sm-4"><div class="card"><div class="card-body"><div class="subheader">Top-Level Items</div><div class="h1 mb-0 mt-2">%d</div></div></div></div>
  <div class="col-sm-4"><div class="card"><div class="card-body"><div class="subheader">Total Menu Items</div><div class="h1 mb-0 mt-2">%d</div></div></div></div>
  <div class="col-sm-4"><div class="card"><div class="card-body"><div class="subheader">Layout Status</div><div class="mt-2"><span class="badge bg-green">Active</span></div></div></div></div>
</div>

<div class="row g-4">
  <div class="col-lg-8">
    <div class="card">
      <div class="card-header d-flex justify-content-between align-items-center">
        <h3 class="card-title mb-0 fw-bold">Admin Navigation Layout</h3>
        <span class="badge bg-blue-lt" id="mm-count"></span>
      </div>
      <div class="card-body p-3">
        <ul class="mm-tree" id="mm-root"></ul>
      </div>
      <div class="card-footer bg-transparent d-flex justify-content-between align-items-center flex-wrap gap-2">
        <div class="d-flex gap-2">
          <button onclick="mmSave()" class="btn btn-primary fw-bold" id="mm-save-btn">Save Layout</button>
          <button onclick="mmReset()" class="btn btn-outline-danger fw-bold">Reset to Defaults</button>
        </div>
        <span class="text-muted small">Drag to reorder • Nest items into groups for dropdowns</span>
      </div>
    </div>

    <div class="card mt-3">
      <div class="card-header"><h3 class="card-title mb-0 fw-bold">Live Preview</h3></div>
      <div class="card-body p-3">
        <div class="mm-preview" id="mm-preview"></div>
      </div>
    </div>
  </div>

  <div class="col-lg-4">
    <div class="card mb-3">
      <div class="card-header"><h3 class="card-title mb-0 fw-bold">Icon Picker</h3></div>
      <div class="card-body">
        <input type="text" class="form-control mb-3" id="mm-icon-search" placeholder="Search icons..." oninput="mmFilterIcons()">
        <div class="mm-icon-grid" id="mm-icon-grid"></div>
      </div>
    </div>

    <div class="card mb-3">
      <div class="card-header"><h3 class="card-title mb-0 fw-bold">Edit Item</h3></div>
      <div class="card-body" id="mm-edit-panel">
        <p class="text-muted small text-center py-3">Click a menu item's ✏️ button to edit.</p>
      </div>
    </div>

    <div class="card mb-3">
      <div class="card-header"><h3 class="card-title mb-0 fw-bold">Add Group Container</h3></div>
      <div class="card-body">
        <div class="mb-3">
          <label class="form-label">Group Label</label>
          <input type="text" class="form-control" id="mm-new-label" placeholder="e.g. Tools, Analytics">
        </div>
        <div class="mb-3">
          <label class="form-label">Icon</label>
          <input type="text" class="form-control" id="mm-new-icon" placeholder="e.g. folder" value="folder">
        </div>
        <button onclick="mmAddGroup()" class="btn btn-primary w-100">Add Group</button>
      </div>
    </div>

    <div class="card mb-3">
      <div class="card-header"><h3 class="card-title mb-0 fw-bold">Export / Import</h3></div>
      <div class="card-body">
        <button onclick="mmExport()" class="btn btn-outline-primary w-100 mb-2">Export JSON</button>
        <label class="btn btn-outline-secondary w-100 mb-0">
          Import JSON
          <input type="file" accept=".json" style="display:none" onchange="mmImport(event)">
        </label>
      </div>
    </div>

    <div class="card">
      <div class="card-header"><h3 class="card-title mb-0 fw-bold">Tips</h3></div>
      <div class="card-body">
        <div class="mb-2"><strong>↕️ Reorder:</strong> Drag items up/down</div>
        <div class="mb-2"><strong>📁 Nest:</strong> Drag onto a group for submenus</div>
        <div class="mb-2"><strong>✏️ Edit:</strong> Click edit to rename or change icon</div>
        <div><strong>🗑️ Remove:</strong> Click × to remove from layout</div>
      </div>
    </div>
  </div>
</div>

<div class="mm-toast" id="mm-toast">
  <div class="alert alert-success alert-dismissible shadow-lg border-0" role="alert">
    <div id="mm-toast-msg">Saved!</div>
    <a class="btn-close" onclick="document.getElementById('mm-toast').classList.remove('show')"></a>
  </div>
</div>

<script src="https://cdn.jsdelivr.net/npm/sortablejs@1.15.6/Sortable.min.js"></script>
<script>
let mmData = %s;
let mmDefaults = %s;
let mmDirty = false;
let mmEditTarget = null;

const ICONS = [
  "home","file-text","palette","settings","users","plug","menu-2","folder",
  "layout-grid","box","chart-bar","chart-line","chart-pie","database",
  "mail","message","bell","star","heart","bookmark","flag","tag","tags",
  "search","filter","adjustments","tool","wrench","shield","lock","key",
  "globe","world","link","external-link","cloud","upload","download",
  "photo","camera","video","music","headphones","microphone","eye",
  "clock","calendar","map","map-pin","compass","rocket","bolt","bulb",
  "code","terminal","git-branch","bug","test-pipe","clipboard","list",
  "list-check","checklist","note","notebook","book","news","rss",
  "share","brand-twitter","brand-github","brand-google","sitemap",
  "arrows-sort","menu-order","hierarchy","binary-tree","arrow-right",
  "arrow-left","chevron-right","chevron-down","dots","grip-vertical",
  "trash","pencil","plus","minus","x","check","info-circle","alert-circle",
  "help-circle","question-mark","user","user-plus","users-group",
  "building","briefcase","shopping-cart","credit-card","receipt","coin",
  "currency-dollar","chart-dots","trending-up","activity","gauge",
  "cpu","server","wifi","bluetooth","printer","device-desktop",
  "device-mobile","device-tablet","browser","app-window","apps"
];

function esc(s){return s?s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;'):''}

function mmRender(){
  const root=document.getElementById('mm-root');
  root.innerHTML='';
  mmData.forEach(function(item,i){root.appendChild(mmNode(item))});
  mmInitSort();
  mmUpdateCount();
  mmUpdatePreview();
}

function mmNode(item){
  const li=document.createElement('li');
  li.className='mm-item';
  li.dataset.label=item.Label||'';
  li.dataset.url=item.URL||'';
  li.dataset.icon=item.Icon||'';
  const isGroup=!item.URL;
  const hasKids=item.Children&&item.Children.length>0;
  var html='<div class="mm-nest-bar"></div><div class="mm-header">';
  html+='<div class="mm-handle"><svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" fill="none"><path d="M9 5m-1 0a1 1 0 1 0 2 0a1 1 0 1 0-2 0"/><path d="M9 12m-1 0a1 1 0 1 0 2 0a1 1 0 1 0-2 0"/><path d="M9 19m-1 0a1 1 0 1 0 2 0a1 1 0 1 0-2 0"/><path d="M15 5m-1 0a1 1 0 1 0 2 0a1 1 0 1 0-2 0"/><path d="M15 12m-1 0a1 1 0 1 0 2 0a1 1 0 1 0-2 0"/><path d="M15 19m-1 0a1 1 0 1 0 2 0a1 1 0 1 0-2 0"/></svg></div>';
  if(item.Icon)html+='<div class="mm-item-icon" style="flex-shrink:0;color:var(--tblr-primary)"><svg class="icon" width="20" height="20"><use href="/static/tabler-sprite.svg#tabler-'+esc(item.Icon)+'"/></svg></div>';
  html+='<div class="flex-fill"><div class="mm-label">'+esc(item.Label)+'</div>';
  if(item.URL)html+='<div class="mm-url">'+esc(item.URL)+'</div>';
  html+='</div>';
  if(isGroup)html+='<span class="badge bg-purple-lt">Group</span>';
  if(hasKids)html+='<span class="badge bg-blue-lt">'+item.Children.length+'</span>';
  html+='<button class="btn btn-sm btn-ghost-primary p-1" onclick="mmEdit(this.closest(\'.mm-item\'))" title="Edit">✏️</button>';
  html+='<button class="btn btn-sm btn-ghost-danger p-1" onclick="mmRemove(this.closest(\'.mm-item\'))" title="Remove">✕</button>';
  html+='</div>';
  li.innerHTML=html;
  const sub=document.createElement('ul');
  sub.className='mm-tree mm-zone';
  if(hasKids)item.Children.forEach(function(c){sub.appendChild(mmNode(c))});
  li.appendChild(sub);
  return li;
}

function mmInitSort(){
  document.querySelectorAll('.mm-tree').forEach(function(el){
    if(el._sortable)el._sortable.destroy();
    el._sortable=new Sortable(el,{
      group:'mm-menus',animation:200,fallbackOnBody:true,swapThreshold:0.65,
      handle:'.mm-handle',ghostClass:'sortable-ghost',
      onEnd:function(){mmUpdateCount();mmMarkDirty();mmUpdatePreview()}
    });
  });
}

function mmSerialize(root){
  const items=[];
  for(const li of root.children){
    if(!li.classList.contains('mm-item'))continue;
    const item={Label:li.dataset.label||'',URL:li.dataset.url||'',Icon:li.dataset.icon||''};
    const sub=li.querySelector(':scope>.mm-tree');
    if(sub&&sub.children.length>0)item.Children=mmSerialize(sub);
    items.push(item);
  }
  return items;
}

async function mmSave(){
  const btn=document.getElementById('mm-save-btn');
  btn.disabled=true;btn.textContent='Saving...';
  const layout=mmSerialize(document.getElementById('mm-root'));
  try{
    const r=await fetch('/admin/arrange-menus/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(layout)});
    const d=await r.json();
    if(r.ok){mmToast('Menu layout saved! Refresh to see changes.','success');mmClearDirty()}
    else mmToast('Error: '+(d.error||'Unknown'),'danger');
  }catch(e){mmToast('Network error: '+e.message,'danger')}
  btn.disabled=false;btn.textContent='Save Layout';
}

async function mmReset(){
  if(!confirm('Reset admin menu to defaults? This cannot be undone.'))return;
  try{
    const r=await fetch('/admin/arrange-menus/reset',{method:'POST'});
    if(r.ok){mmToast('Reset! Refreshing...','success');setTimeout(function(){location.reload()},1000)}
  }catch(e){mmToast('Error: '+e.message,'danger')}
}

function mmAddGroup(){
  const label=document.getElementById('mm-new-label').value.trim();
  if(!label){alert('Enter a group label');return}
  const icon=document.getElementById('mm-new-icon').value.trim()||'folder';
  const root=document.getElementById('mm-root');
  root.appendChild(mmNode({Label:label,URL:'',Icon:icon,Children:[]}));
  mmInitSort();mmUpdateCount();mmMarkDirty();
  document.getElementById('mm-new-label').value='';
}

function mmEdit(li){
  mmEditTarget=li;
  document.querySelectorAll('.mm-item').forEach(function(el){el.style.outline=''});
  li.style.outline='2px solid var(--tblr-primary)';
  var h='<div class="mb-3"><label class="form-label">Label</label><input type="text" class="form-control" id="mm-e-label" value="'+esc(li.dataset.label)+'"></div>';
  h+='<div class="mb-3"><label class="form-label">URL</label><input type="text" class="form-control font-monospace" id="mm-e-url" value="'+esc(li.dataset.url)+'" placeholder="Empty = group container"></div>';
  h+='<div class="mb-3"><label class="form-label">Icon</label><input type="text" class="form-control" id="mm-e-icon" value="'+esc(li.dataset.icon)+'"><small class="text-muted">Click an icon in the picker above</small></div>';
  h+='<button class="btn btn-primary w-100" onclick="mmApplyEdit()">Apply Changes</button>';
  document.getElementById('mm-edit-panel').innerHTML=h;
}

function mmApplyEdit(){
  if(!mmEditTarget)return;
  mmEditTarget.dataset.label=document.getElementById('mm-e-label').value;
  mmEditTarget.dataset.url=document.getElementById('mm-e-url').value;
  mmEditTarget.dataset.icon=document.getElementById('mm-e-icon').value;
  var lbl=mmEditTarget.querySelector('.mm-label');
  var url=mmEditTarget.querySelector('.mm-url');
  if(lbl)lbl.textContent=mmEditTarget.dataset.label;
  if(url)url.textContent=mmEditTarget.dataset.url;
  var iconEl=mmEditTarget.querySelector('.mm-item-icon');
  if(mmEditTarget.dataset.icon){
    var iconHtml='<svg class="icon" width="20" height="20"><use href="/static/tabler-sprite.svg#tabler-'+esc(mmEditTarget.dataset.icon)+'"/></svg>';
    if(iconEl){iconEl.innerHTML=iconHtml}else{
      var d=document.createElement('div');d.className='mm-item-icon';d.style.cssText='flex-shrink:0;color:var(--tblr-primary)';d.innerHTML=iconHtml;
      var handle=mmEditTarget.querySelector('.mm-handle');
      if(handle)handle.parentNode.insertBefore(d,handle.nextSibling);
    }
  }else if(iconEl){iconEl.remove()}
  mmEditTarget.style.outline='';
  mmMarkDirty();mmUpdatePreview();
  mmToast('Item updated. Click Save Layout to persist.','info');
}

function mmRemove(li){
  if(!confirm('Remove "'+li.dataset.label+'" from layout?'))return;
  li.remove();mmUpdateCount();mmMarkDirty();mmUpdatePreview();
}

function mmMarkDirty(){mmDirty=true;const b=document.getElementById('mm-save-btn');b.classList.add('btn-warning');b.classList.remove('btn-primary')}
function mmClearDirty(){mmDirty=false;const b=document.getElementById('mm-save-btn');b.classList.remove('btn-warning');b.classList.add('btn-primary')}
function mmUpdateCount(){
  const t=document.querySelectorAll('.mm-item').length;
  const tl=document.getElementById('mm-root').children.length;
  document.getElementById('mm-count').textContent=tl+' top-level, '+t+' total';
}

function mmUpdatePreview(){
  const layout=mmSerialize(document.getElementById('mm-root'));
  const p=document.getElementById('mm-preview');
  p.innerHTML='';
  layout.forEach(function(item){
    const el=document.createElement('div');
    el.className='mm-preview-item';
    el.textContent=item.Label+(item.Children&&item.Children.length?' \u25BE':'');
    p.appendChild(el);
  });
}

function mmToast(msg,type){
  var t=document.getElementById('mm-toast');
  t.querySelector('.alert').className='alert alert-'+(type||'success')+' alert-dismissible shadow-lg border-0';
  document.getElementById('mm-toast-msg').textContent=msg;
  t.classList.add('show');
  setTimeout(function(){t.classList.remove('show')},4000);
}

// Icon picker
function mmRenderIcons(){
  const grid=document.getElementById('mm-icon-grid');
  grid.innerHTML='';
  ICONS.forEach(function(name){
    const btn=document.createElement('div');
    btn.className='mm-icon-btn';
    btn.dataset.name=name;
    btn.title=name;
    btn.innerHTML='<svg class="icon" width="20" height="20"><use href="/static/tabler-sprite.svg#tabler-'+name+'"/></svg><div style="font-size:9px;margin-top:2px;overflow:hidden;text-overflow:ellipsis">'+name+'</div>';
    btn.onclick=function(){
      document.querySelectorAll('.mm-icon-btn').forEach(function(b){b.classList.remove('active')});
      btn.classList.add('active');
      var iconInput=document.getElementById('mm-e-icon');
      if(iconInput)iconInput.value=name;
      var newIconInput=document.getElementById('mm-new-icon');
      if(newIconInput)newIconInput.value=name;
    };
    grid.appendChild(btn);
  });
}

function mmFilterIcons(){
  var q=document.getElementById('mm-icon-search').value.toLowerCase();
  document.querySelectorAll('.mm-icon-btn').forEach(function(btn){
    btn.style.display=btn.dataset.name.includes(q)?'':'none';
  });
}

function mmExport(){
  const layout=mmSerialize(document.getElementById('mm-root'));
  const blob=new Blob([JSON.stringify(layout,null,2)],{type:'application/json'});
  const a=document.createElement('a');
  a.href=URL.createObjectURL(blob);
  a.download='backend_menus.json';
  a.click();
}

function mmImport(e){
  const file=e.target.files[0];
  if(!file)return;
  const reader=new FileReader();
  reader.onload=function(ev){
    try{
      mmData=JSON.parse(ev.target.result);
      mmRender();mmMarkDirty();
      mmToast('Layout imported. Click Save to apply.','success');
    }catch(err){mmToast('Invalid JSON file','danger')}
  };
  reader.readAsText(file);
}

window.addEventListener('beforeunload',function(e){if(mmDirty){e.preventDefault();e.returnValue='Unsaved changes'}});

// Init
mmRenderIcons();
mmRender();
</script>`, topLevel, totalItems, string(menusJSON), string(defaultsJSON))
}

func countItems(items []plugin.MenuItem) int {
	count := len(items)
	for _, item := range items {
		if item.Children != nil {
			count += countItems(item.Children)
		}
	}
	return count
}

func main() {
	p := &MenuManagerPlugin{}
	p.initDB()

	hplugin.Serve(&hplugin.ServeConfig{
		HandshakeConfig: plugin.HandshakeConfig,
		Plugins: map[string]hplugin.Plugin{
			"cms_plugin": &plugin.CMSPluginDef{Impl: p},
		},
	})
}
