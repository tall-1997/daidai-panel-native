package handler

import (
	"encoding/json"
	"errors"
	"sync"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 当前登录用户的界面偏好读写（issue #116-5：换设备 / 换浏览器也记得住）。
//
// 落在 auth 组而不是 configs 组，是因为 /api/configs 整组要求 admin，
// 而脚本页只要 operator —— 详见 model/user_preference.go 顶部那段。
//
// 目前有两组，各占 user_preferences 的一列 JSON：
//   - editor：编辑器开关（Editor 列），整套默认值 + 组级 stored；
//   - list：列表页 / 日志查看等界面偏好（List 列，issue #143 起，#147 加了日志查看），稀疏存储、服务端不维护默认值。
//
// GET / PUT 的响应同形：{editor: 整套值, stored: editor 组存过没有, list: 只含存过的键}。
// PUT 按组可选写入：请求里带了哪组才写哪组的列，**没带的那组一个字节都不碰**。

// editorPreferences 与 web/src/utils/editorPreferences.ts 的 EditorPreferences 逐字对应，
// 改一边必须改另一边（键名、取值范围、默认值三样都要对齐）。
//
// ⚠️ minimap / indent_guides 下发时是 JSON 布尔：前端 ensureEditorPreferencesLoaded() 用
// `typeof editor.minimap === 'boolean'` 判定该不该写回本地缓存，下发成字符串它会整项忽略。
// 而 indent_width 下发的是**字符串**（"auto" / "2" / "4" / "6" / "8"），
// 因为它在前端是 'auto' | number 的联合类型，统一走字符串两边都不用做类型分支。
type editorPreferences struct {
	WordWrap     string `json:"word_wrap"`
	Minimap      bool   `json:"minimap"`
	IndentGuides bool   `json:"indent_guides"`
	Whitespace   string `json:"whitespace"`
	IndentWidth  string `json:"indent_width"`
}

// 默认值必须与 web/src/utils/editorPreferences.ts 的 EDITOR_PREFERENCES_DEFAULTS 逐字相同。
// 不一致的表现是：从没存过偏好的用户打开编辑器，本地缓存先按前端默认渲染一次，
// 服务端值拉回来的瞬间观感又跳一下 —— 不报错、构建全绿，只有用户看得见。
// ⚠️ 改这里要同步改 editorPreferences.ts。
const (
	editorDefaultWordWrap     = "on"
	editorDefaultMinimap      = false
	editorDefaultIndentGuides = true
	editorDefaultWhitespace   = "selection"
	editorDefaultIndentWidth  = "auto"
)

// Editor 列的长度兜底。这一列只装 5 个短枚举值，正常撑死一百来字节；
// 4KB 是留给「将来多几个开关」的余量，同时挡住被改坏的客户端往里灌大字符串。
const editorPreferenceMaxBytes = 4 * 1024

func defaultEditorPreferences() editorPreferences {
	return editorPreferences{
		WordWrap:     editorDefaultWordWrap,
		Minimap:      editorDefaultMinimap,
		IndentGuides: editorDefaultIndentGuides,
		Whitespace:   editorDefaultWhitespace,
		IndentWidth:  editorDefaultIndentWidth,
	}
}

// 各枚举项的合法取值，校验与报错文案共用同一份，避免两边写岔。
var (
	editorWordWrapValues    = []string{"on", "off"}
	editorWhitespaceValues  = []string{"none", "selection", "all"}
	editorIndentWidthValues = []string{"auto", "2", "4", "6", "8"}
)

func editorValueAllowed(allowed []string, value string) bool {
	for _, item := range allowed {
		if item == value {
			return true
		}
	}
	return false
}

// editorFlag 是 minimap / indent_guides 的**入参**类型：既吃 JSON 布尔，也吃 "on" / "off" 字符串。
//
// 面板前端提交的是 JSON 布尔：web/src/utils/editorPreferences.ts 的 setEditorPreference 里
// 只有这两个键走 `value as boolean`，web/src/api/auth.ts 的 updatePreferences 签名也是
// Record<string, string | boolean>。所以 *bool 就够面板自己用了。
//
// 额外认 "on" / "off" / "true" / "false" 是给**面板之外**的客户端留的：
// APP、第三方脚本、以及把这两项当字符串开关发上来的历史客户端。
// 它们发字符串时如果只声明 *bool，反序列化会直接 400，而这类客户端对同步失败往往是静默的 ——
// 表现为这两个开关跨端永远不生效，一行报错都看不到。TestUpdateEditorPreferencesAcceptsOnOffFlags
// 覆盖的就是这条兼容路径（不是面板自己的路径）。
// 下发方向不受影响：GET 回去的永远是 JSON 布尔。
type editorFlag bool

// 反序列化阶段的取值错误只能从 ShouldBindJSON 里出来，用哨兵值把它捞出来单独回文案，
// 否则用户只会看到一句「请求参数错误」，不知道是哪一项、也不知道该填什么。
var errEditorFlagValue = errors.New("minimap / indent_guides 取值需为 true、false、on 或 off")

func (f *editorFlag) UnmarshalJSON(data []byte) error {
	var asBool bool
	if err := json.Unmarshal(data, &asBool); err == nil {
		*f = editorFlag(asBool)
		return nil
	}

	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		switch asString {
		case "on", "true":
			*f = true
			return nil
		case "off", "false":
			*f = false
			return nil
		}
	}

	return errEditorFlagValue
}

// editorPreferencesPatch 是 PUT 的入参：指针为 nil 表示这一项不改。
// 字段级合并是刻意的 —— 两个标签页各改各的开关时，后提交的那个不能把前一个的改动盖回去
// （前端每次只提交被点的那一个键，正是为了配合这里）。
type editorPreferencesPatch struct {
	WordWrap     *string     `json:"word_wrap"`
	Minimap      *editorFlag `json:"minimap"`
	IndentGuides *editorFlag `json:"indent_guides"`
	Whitespace   *string     `json:"whitespace"`
	IndentWidth  *string     `json:"indent_width"`
}

// validateEditorPreferencesPatch 逐项校验 PUT 入参里的 editor 组，返回给用户看的报错文案；全部合法时返回空串。
// 与合并拆开：校验不依赖库里的旧值，UpdatePreferences 才能在读库、拿写锁之前就把非法请求 400 掉。
// minimap / indent_guides 的取值错误在反序列化阶段就拦下了，这里不用再管。
func validateEditorPreferencesPatch(patch *editorPreferencesPatch) string {
	if patch.WordWrap != nil && !editorValueAllowed(editorWordWrapValues, *patch.WordWrap) {
		return "word_wrap 取值需为 on 或 off"
	}
	if patch.Whitespace != nil && !editorValueAllowed(editorWhitespaceValues, *patch.Whitespace) {
		return "whitespace 取值需为 none、selection 或 all"
	}
	if patch.IndentWidth != nil && !editorValueAllowed(editorIndentWidthValues, *patch.IndentWidth) {
		return "indent_width 取值需为 auto、2、4、6 或 8"
	}
	return ""
}

// mergeEditorPreferences 把入参里带了的项覆盖到 base 上，没带的项保持原值。调用前必须先过 validateEditorPreferencesPatch。
func mergeEditorPreferences(base editorPreferences, patch *editorPreferencesPatch) editorPreferences {
	if patch.WordWrap != nil {
		base.WordWrap = *patch.WordWrap
	}
	if patch.Whitespace != nil {
		base.Whitespace = *patch.Whitespace
	}
	if patch.IndentWidth != nil {
		base.IndentWidth = *patch.IndentWidth
	}
	if patch.Minimap != nil {
		base.Minimap = bool(*patch.Minimap)
	}
	if patch.IndentGuides != nil {
		base.IndentGuides = bool(*patch.IndentGuides)
	}
	return base
}

// listPreferences 是「列表页 / 日志查看等界面偏好」那一组（稀疏）。组名沿用 v3.3.1 的 list，
// 但它早已不只装列表页：issue #143 桌面端第 1 条放进来任务页 / 环境变量页的每页条数、视图栏「全部」「分组标签」显隐，
// #147 又放进来「打开已结束的日志时定位到底部」。以后再有跟账户走的零散界面开关，照样往这里加键，
// 不要按组名字面另开一组 —— 否则同一类偏好会出现两处真源。
// 键名与取值范围必须与 web/src/utils/listPreferences.ts 的 ListPreferences 逐键对齐。
//
// 这一组刻意**不**照抄 editor 的「整套默认值 + 组级 stored」，而是稀疏存储 —— 只存用户显式设过的键：
//  1. 服务端不维护默认值，也就没有「前后端两份默认值必须逐字对齐」这个不变式，默认值只在前端一处；
//  2. 迁移能精确到每一个键。localStorage 按 origin 隔离，多域名 / 多 IP 在用的用户每个 origin 各有一份老值。
//     组级 stored 下，第一个被打开的 origin 哪怕从没改过，也会把默认值「占坑」写上来，
//     其它 origin 的自定义值随后全被冲掉；稀疏存储下前端只在「服务端没有这个键、本机老键里真有值」时上行那一个键。
//
// 同一个类型兼任三个角色：List 列的存储形状、响应里的 list、PUT 入参里的 list。
// 能兼任是因为稀疏存储下「没存过」和「这次不改」都用 nil 表达，语义正好重合。
// 全部是指针 + omitempty：nil 下发时整键省略；而指向 false 的 *bool 照常下发（omitempty 只看指针是否为 nil），
// 所以「用户显式设成不隐藏」与「从没设过」下发时是两种形态，前端能分清。
//
// ⚠️ 类型是契约：tasks_page_size 是 JSON number；envs_page_size 是 JSON string（它有 "all" 这个值，只能走字符串）；
// 两个 hidden 与 log_open_at_bottom 是 JSON bool，且不像 editorFlag 那样兼容 "on"/"off" —— 这是新接口，没有要照顾的历史客户端。
// 类型不对的入参由 ShouldBindJSON 直接回 400。
type listPreferences struct {
	TasksPageSize         *int    `json:"tasks_page_size,omitempty"`
	EnvsPageSize          *string `json:"envs_page_size,omitempty"`
	TasksViewAllHidden    *bool   `json:"tasks_view_all_hidden,omitempty"`
	TasksViewGroupsHidden *bool   `json:"tasks_view_groups_hidden,omitempty"`
	// #147：打开【已结束】的日志时是否直接定位到底部，面板网页与 APP 共用这一个键。
	// 没存过时服务端不下发，各端按自己的现状处理（网页停在顶部，与 v3.2.8 / #133 起一致）。
	// 运行中的日志一律自动跟随，与它无关。
	LogOpenAtBottom *bool `json:"log_open_at_bottom,omitempty"`
}

// List 列的长度兜底，口径同 editorPreferenceMaxBytes。
// 现在白名单里只有 5 个键，编码后撑死一百来字节，这条上限实际碰不到；
// 留着是为了将来加键时仍有一道「挡住异常大值」的闸，而不是依赖白名单永远这么短。
const listPreferenceMaxBytes = 4 * 1024

// 各键的合法取值。与前端 TASKS_PAGE_SIZE_OPTIONS / ENVS_PAGE_SIZE_OPTIONS 逐项对齐。
var (
	listTasksPageSizeValues = []int{10, 20, 50, 100}
	listEnvsPageSizeValues  = []string{"20", "50", "100", "all"}
)

func listValueAllowed[T comparable](allowed []T, value T) bool {
	for _, item := range allowed {
		if item == value {
			return true
		}
	}
	return false
}

// empty 回答「这一组里一个键都没有」。PUT 里用它判定 list 组是否需要落库。
// ⚠️ 加键时这里必须同步加：漏了，只带新键的 PUT 会被判成空补丁，200 静默 no-op。
func (p listPreferences) empty() bool {
	return p.TasksPageSize == nil && p.EnvsPageSize == nil &&
		p.TasksViewAllHidden == nil && p.TasksViewGroupsHidden == nil &&
		p.LogOpenAtBottom == nil
}

// validateListPreferencesPatch 逐键校验 PUT 入参里的 list 组，返回给用户看的报错文案；全部合法时返回空串。
// 文案按字段分开写（与 editor 组一致），调用方才知道是哪一项、该填什么。
// 两个 hidden 与 log_open_at_bottom 是 bool，类型对了就没有非法值可言，不用在这里校验。
func validateListPreferencesPatch(patch *listPreferences) string {
	if patch.TasksPageSize != nil && !listValueAllowed(listTasksPageSizeValues, *patch.TasksPageSize) {
		return "tasks_page_size 取值需为 10、20、50 或 100"
	}
	if patch.EnvsPageSize != nil && !listValueAllowed(listEnvsPageSizeValues, *patch.EnvsPageSize) {
		return "envs_page_size 取值需为 20、50、100 或 all"
	}
	return ""
}

// mergeListPreferences 把入参里带了的键覆盖到 base 上，没带的键保持原值（字段级合并，理由同 editorPreferencesPatch）。
func mergeListPreferences(base listPreferences, patch *listPreferences) listPreferences {
	if patch.TasksPageSize != nil {
		base.TasksPageSize = patch.TasksPageSize
	}
	if patch.EnvsPageSize != nil {
		base.EnvsPageSize = patch.EnvsPageSize
	}
	if patch.TasksViewAllHidden != nil {
		base.TasksViewAllHidden = patch.TasksViewAllHidden
	}
	if patch.TasksViewGroupsHidden != nil {
		base.TasksViewGroupsHidden = patch.TasksViewGroupsHidden
	}
	if patch.LogOpenAtBottom != nil {
		base.LogOpenAtBottom = patch.LogOpenAtBottom
	}
	return base
}

// decodeListKey 把 List 列里某一个键的原始 JSON 解到 *T。
// 解到指针上而不是值上，是为了把 JSON null 认成「没有这个键」：
// null 解到 bool 值上既不报错也不改零值，会被误当成用户显式存了 false。
func decodeListKey[T any](object map[string]json.RawMessage, key string) *T {
	data, exists := object[key]
	if !exists {
		return nil
	}
	var value *T
	if err := json.Unmarshal(data, &value); err != nil {
		return nil
	}
	return value
}

// decodeListPreferences 把 List 列解析成稀疏的 listPreferences。
//
// 口径与 editor 一样「脏数据不报错」：空串、JSON 解析失败、不是 JSON 对象，一律当 {}（一个键都没存过）。
// 这一列纯粹是观感偏好，让一条脏数据把任务页、环境变量页打不开，代价远大于悄悄回落前端默认。
// 是对象时**逐键**过类型与白名单，只丢弃脏的那一个键，其余照用；白名单外的键同样丢弃，不下发。
// 逐键解析而不是整体 Unmarshal 到结构体：encoding/json 碰到某个键类型不符会返回错误，
// 虽然它仍会「尽力」填好其余字段，但靠这种半成功语义决定去留太隐晦。
func decodeListPreferences(raw string) listPreferences {
	var result listPreferences
	if raw == "" {
		return result
	}

	// 解到 map 上时 "null" 不报错、map 为 nil，一并当「不是对象」处理。
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &object); err != nil || object == nil {
		return result
	}

	if value := decodeListKey[int](object, "tasks_page_size"); value != nil &&
		listValueAllowed(listTasksPageSizeValues, *value) {
		result.TasksPageSize = value
	}
	if value := decodeListKey[string](object, "envs_page_size"); value != nil &&
		listValueAllowed(listEnvsPageSizeValues, *value) {
		result.EnvsPageSize = value
	}
	result.TasksViewAllHidden = decodeListKey[bool](object, "tasks_view_all_hidden")
	result.TasksViewGroupsHidden = decodeListKey[bool](object, "tasks_view_groups_hidden")
	result.LogOpenAtBottom = decodeListKey[bool](object, "log_open_at_bottom")
	return result
}

// currentPreferenceUser 取当前登录用户。偏好是 per-user 的，拿不到用户就没有可读写的对象。
func currentPreferenceUser(c *gin.Context) (*model.User, bool) {
	username, _ := c.Get("username")

	var user model.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		response.NotFound(c, "用户不存在")
		return nil, false
	}
	return &user, true
}

// loadPreferenceRecord 一次查询取出某个用户的整行偏好，GET / PUT 的各组共用，不必每组各查一遍。
// 没有行不算错误：返回零值记录，Editor / List 都是空串，即各组都「没存过」。
// 其它数据库错误原样返回，由调用方决定是回落（GET）还是中止（PUT）。
func loadPreferenceRecord(userID uint) (model.UserPreference, error) {
	var record model.UserPreference
	err := database.DB.Where("user_id = ?", userID).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.UserPreference{}, nil
	}
	if err != nil {
		return model.UserPreference{}, err
	}
	return record, nil
}

// decodeEditorPreferences 把 Editor 列解析成编辑器偏好。
//
// 第一个返回值永远是「一整套可用的值」：记录不存在、Editor 是空串、JSON 解析失败 ——
// 一律回落默认而**不报错**。这一列纯粹是观感偏好，服务端没有任何运行时逻辑依赖它，
// 让一条脏数据把编辑器页面整个打不开，代价远大于「悄悄回落默认」。
// 解析成功但某一项是枚举外的脏值时，只让那一项回落默认，其余照用。
//
// 第二个返回值 stored 回答的是另一个问题：**这套值到底是用户存的，还是我们现编的默认值**。
// 光看第一个返回值分不出来，因为「从没存过」和「用户主动把 5 项都设成默认值」下发的字节完全一样。
// 这个区分不是可有可无的洁癖，它挡的是一个每个升级用户都会撞上的数据丢失：
// 老用户在 v3.2.2/3.2.3 把偏好存在浏览器本地（dd:editor:*），升级后第一次打开编辑器，
// 前端 ensureEditorPreferencesLoaded() 会拿服务端下发的值逐项写回本地缓存 ——
// 服务端这边其实一行记录都没有、下发的全是默认值，于是本机那份老偏好被默认值**无条件冲掉**，
// 而且是覆盖式写入、刷新也回不来，正好和 editorPreferences.ts 文件头「升级后偏好平滑保留」的承诺相反。
// 有了 stored，前端就能在 stored=false 时反过来把本机那份 PUT 上去（首次上行迁移）。
// ⚠️ 所以它不是冗余字段，删掉就等于把上面那条数据丢失重新放回来。
// ⚠️ 同理，只写 list 组的 PUT 绝不能碰 Editor 列：一旦往里写了默认值，stored 就被翻成 true，
// 上面那条数据丢失原样回来（见 UpdatePreferences 的按组 upsert）。
//
// 判据（与 web 端逐字对齐）：有行 + Editor 非空 + 能解析成 JSON 对象且装得进 editorPreferences。
// 后两条不满足时一律当「没存过」——反正这种行里也读不出任何可用的用户选择，
// 让前端把本机那份重新迁上来，比拿默认值把它盖掉划算得多。
// 「没有行」由调用方传空串进来表达（loadPreferenceRecord 的零值记录）。
func decodeEditorPreferences(raw string) (editorPreferences, bool) {
	prefs := defaultEditorPreferences()

	if raw == "" {
		return prefs, false
	}

	// 先确认它确实是个 JSON 对象。不能只看下面那次 Unmarshal 报没报错：
	// Editor 存成 "null" 时，解到结构体上既不报错也不改任何字段，
	// 会被误判成「用户存过一套刚好等于默认值的偏好」。数组、裸字符串、裸数字同理（那些会报错）。
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &object); err != nil || object == nil {
		return prefs, false
	}

	// 反序列化到「已经填好默认值」的结构体上：JSON 里缺的键保持默认，
	// 这样服务端新增一项开关时，老记录不会因为缺键而拿到 Go 零值（比如 indent_guides 变 false）。
	// 变量叫 decoded 不叫 stored：stored 现在是「存过没有」那个返回值的名字，两者别混。
	decoded := prefs
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return prefs, false
	}

	if editorValueAllowed(editorWordWrapValues, decoded.WordWrap) {
		prefs.WordWrap = decoded.WordWrap
	}
	if editorValueAllowed(editorWhitespaceValues, decoded.Whitespace) {
		prefs.Whitespace = decoded.Whitespace
	}
	if editorValueAllowed(editorIndentWidthValues, decoded.IndentWidth) {
		prefs.IndentWidth = decoded.IndentWidth
	}
	// 布尔项没有「枚举外的值」可言，解析出来是什么就是什么。
	prefs.Minimap = decoded.Minimap
	prefs.IndentGuides = decoded.IndentGuides

	// 单项枚举外的脏值（比如 whitespace 存成 "boundary"）不影响这里的 stored：
	// 用户确实存过，只是那一项回落默认，其余仍然是他自己的选择。
	return prefs, true
}

// preferencesPayload 拼 GET / PUT 共用的响应体，两者必须同形。
// list 传值类型：一个键都没有时编码成 {} 而不是 null，前端「list 不是对象就当老服务端」的判定才不会误伤。
func preferencesPayload(editor editorPreferences, stored bool, list listPreferences) gin.H {
	return gin.H{"editor": editor, "stored": stored, "list": list}
}

func (h *AuthHandler) GetPreferences(c *gin.Context) {
	user, ok := currentPreferenceUser(c)
	if !ok {
		return
	}

	// 读库失败时按「没有行」处理，与加 list 之前的口径一致：偏好读不出来也绝不让页面 500。
	record, _ := loadPreferenceRecord(user.ID)

	// editor 无论如何都下发一整套可用的值，与加 stored 之前逐字一致 ——
	// 不认得 stored / list 的老客户端（APP / 历史前端）行为完全不变。
	editor, stored := decodeEditorPreferences(record.Editor)
	response.Success(c, preferencesPayload(editor, stored, decodeListPreferences(record.List)))
}

// preferenceWriteMu 把 UpdatePreferences 的「读整行 → 合并 → 整列写回」串成一段。
//
// 为什么非加不可：upsert 写回的是合并后的**整列** JSON，不是单个键。两个只改不同键的 PUT
// （视图管理里同一次保存勾两个隐藏项、两个标签页各改各的、首次迁移紧跟着改每页条数）
// 如果都先读到旧行、再先后写回，后写的那个会把先写的那个键改回旧值 —— 服务端静默丢了一个设置，
// 下次加载时前端又拿这个旧值冲掉本机缓存，用户看到的是设置「自己变回去了」。
// database.go 的 SetMaxOpenConns(1) 只让单条语句轮流用连接，挡不住两段读-改-写在语句之间交错。
//
// 用进程内锁而不是事务：SQLite 文件只有这一个面板进程在写，进程内锁就够；换成 database.DB.Transaction 的话，
// 事务里任何一处用了 database.DB 而不是 tx（loadPreferenceRecord 现在就是），都会在单连接池上
// 等那条被事务自己占住的连接，直接死锁。所有用户共用一把锁：偏好写入频率很低，不值得按用户分锁。
var preferenceWriteMu sync.Mutex

// UpdatePreferences 按组可选写入：请求里带了哪组就写哪组，**没带的那组的列一个字节都不碰**。
//
// 🔴 为什么 Editor 必须是指针、DoUpdates 必须按组动态拼：
// 以前入参是值类型、DoUpdates 写死 editor，于是只发 {"list":{...}}（或 {}）的请求也会把一整套
// editor 默认值写进库，editor 的 stored 从 false 翻成 true。后果是升级用户存在本机 dd:editor:* 的
// 编辑器偏好，下次打开编辑器时被默认值静默冲掉 —— 不报错、测试全绿，只有用户自己看得见。
// 两处漏掉任何一处都会把这条数据丢失放回来，TestUpdateListPreferencesNeverTouchesEditorColumn 钉着它。
func (h *AuthHandler) UpdatePreferences(c *gin.Context) {
	var req struct {
		Editor *editorPreferencesPatch `json:"editor"`
		List   *listPreferences        `json:"list"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// minimap / indent_guides 的取值错误在反序列化阶段就被拦下了，回它自己的文案；
		// 其余（JSON 语法坏了、editor / list 不是对象、list 某一项类型不对之类）走通用文案。
		if errors.Is(err, errEditorFlagValue) {
			response.BadRequest(c, errEditorFlagValue.Error())
			return
		}
		response.BadRequest(c, "请求参数错误")
		return
	}

	// 两组的取值都先校验完，再去读库、拿写锁：任何一组不合法就整单 400，另一组也一个字节不写，
	// 非法请求也不用排进 preferenceWriteMu。
	if req.Editor != nil {
		if message := validateEditorPreferencesPatch(req.Editor); message != "" {
			response.BadRequest(c, message)
			return
		}
	}
	// list 组：一个键都没带（{"list":{}}、或只有白名单外的键）时不写。
	// 与 editor 的空对象不同，稀疏存储下空补丁没有任何东西可存，写下去只会凭空建出一行。
	writeList := req.List != nil && !req.List.empty()
	if writeList {
		if message := validateListPreferencesPatch(req.List); message != "" {
			response.BadRequest(c, message)
			return
		}
	}

	user, ok := currentPreferenceUser(c)
	if !ok {
		return
	}

	// 从读整行到 upsert 落库整段串行，理由见 preferenceWriteMu。
	// defer 解锁：下面「数据过大」「保存失败」这些提前返回都不会把锁带走。
	preferenceWriteMu.Lock()
	defer preferenceWriteMu.Unlock()

	// 整行只读这一次，两组都在它上面合并。
	// 这里读失败必须中止而不是像 GET 那样回落：PUT 是「读-改-写」，
	// 拿一份空记录去合并再写回，会把 list 组里用户之前存过的其它键整片抹掉。
	record, err := loadPreferenceRecord(user.ID)
	if err != nil {
		response.InternalError(c, "读取偏好失败")
		return
	}
	editorPrefs, editorStored := decodeEditorPreferences(record.Editor)
	listPrefs := decodeListPreferences(record.List)

	// 两组都先合并、编码完，最后只落一次库：任何一组超长就整单 400，另一组也一个字节不写。
	upsert := model.UserPreference{UserID: user.ID}
	var columns []string

	// editor 组：只要带了 editor 对象就写 —— 哪怕是空对象 {}，也照旧写入合并后的整套值并把 stored 置 true。
	// 这是加 list 之前就有的行为，只发 editor 的客户端（APP / 历史前端）必须逐字不变。
	if req.Editor != nil {
		editorPrefs = mergeEditorPreferences(editorPrefs, req.Editor)

		encoded, err := json.Marshal(editorPrefs)
		if err != nil {
			response.InternalError(c, "保存编辑器偏好失败")
			return
		}
		if len(encoded) > editorPreferenceMaxBytes {
			response.BadRequest(c, "编辑器偏好数据过大")
			return
		}
		upsert.Editor = string(encoded)
		columns = append(columns, "editor")
	}

	if writeList {
		listPrefs = mergeListPreferences(listPrefs, req.List)

		encoded, err := json.Marshal(listPrefs)
		if err != nil {
			response.InternalError(c, "保存列表偏好失败")
			return
		}
		if len(encoded) > listPreferenceMaxBytes {
			response.BadRequest(c, "列表偏好数据过大")
			return
		}
		upsert.List = string(encoded)
		columns = append(columns, "list")
	}

	// 两组都没带：no-op，原样回当前值、不落库，也不建行。
	// 历史上发 {} 的客户端会拿到 200，只是不再被顺手写进一套 editor 默认值（那正是要修的 bug）。
	if len(columns) == 0 {
		response.Success(c, preferencesPayload(editorPrefs, editorStored, listPrefs))
		return
	}

	// upsert 而不是「先查后建」：一条语句同时覆盖「还没有行」与「已有行」，user_id 上有唯一索引，
	// 万一有绕开 preferenceWriteMu 的写入先建了行，后到的 INSERT 也会走 DO UPDATE 正常落库，而不是撞唯一约束。
	// DoUpdates 只列这次真的写了的组：冲突时没列进来的列保持库里原值。
	// 新建行时没写的那组列留零值，由列定义的 DEFAULT '' 落空串 —— 只写 list 时 editor 仍是「没存过」。
	columns = append(columns, "updated_at")
	if err := database.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns(columns),
	}).Create(&upsert).Error; err != nil {
		response.InternalError(c, "保存偏好失败")
		return
	}

	// 回下发合并后的完整值，前端可以直接拿它覆盖本地缓存，不用自己再拼一次。
	// stored 按 editor 组的真实状态回：这次写了 editor 才一定是 true（写完必然存过，不必回查）；
	// 没写 editor 时沿用这次读到的判定 —— 不能再像以前那样写死 true，
	// 否则只改 list 的请求会让前端误以为 editor 存过，拿默认值冲掉本机那份编辑器偏好。
	if req.Editor != nil {
		editorStored = true
	}
	response.Success(c, preferencesPayload(editorPrefs, editorStored, listPrefs))
}
