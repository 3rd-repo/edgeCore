// Modbus 格式预设 —— 单一事实源（参考 Modbus Slave 分类，按字节数分组）。
// 预设表、点位 → 预设推断、预设 id → format 值映射都从这里出，
// PointList / ModbusSlavePointView / HelpDrawer / PointFormatHelpPanel 一律引用本文件，
// 禁止在其他文件里复制预设表或推断逻辑。
//
// 命名约定（唯一有效命名，不做旧名兼容）：
//   1 字节  位段            UINT8 / INT8 / BIT / BCD8
//   2 字节  1 寄存器        Signed_AB / Signed_BA（AB = 高字节在前，BA = 低字节在前）
//   4 字节  2 寄存器        Signed/Unsigned/Float + ABCD/CDAB/BADC/DCBA
//   8 字节  4 寄存器        Double + 完整 8 字节序列展开（4 寄存器下 4 字母缩写有歧义）：
//           ABCD→ABCDEFGH、CDAB→GHEFCDAB（高低字对调）、
//           BADC→BADCFEHG（每字内字节互换）、DCBA→HGFEDCBA（完全逆序）
export const formatPresets = [
    // ===== 1 字节（位段）=====
    { id: 'UINT8', label: 'UINT8 (8位 / 位段)', bytes: 1, parseType: 'UINT8', wordOrder: 'AB', datatype: 'uint8' },
    { id: 'INT8', label: 'INT8 (8位 / 位段)', bytes: 1, parseType: 'INT8', wordOrder: 'AB', datatype: 'int8' },
    { id: 'BIT', label: 'BIT (8位 / 位段)', bytes: 1, parseType: 'BIT', wordOrder: 'AB', datatype: 'bool' },
    { id: 'BCD8', label: 'BCD8 (8位 / 位段)', bytes: 1, parseType: 'BCD8', wordOrder: 'AB', datatype: 'uint8' },
    // ===== 2 字节（1 寄存器）=====
    { id: 'Signed_AB', label: 'Signed_AB (16位 / 1 寄存器)', bytes: 2, parseType: 'INT16', wordOrder: 'AB', datatype: 'int16' },
    { id: 'Signed_BA', label: 'Signed_BA (16位 / 1 寄存器)', bytes: 2, parseType: 'INT16', wordOrder: 'BA', datatype: 'int16' },
    { id: 'Unsigned_AB', label: 'Unsigned_AB (16位 / 1 寄存器)', bytes: 2, parseType: 'UINT16', wordOrder: 'AB', datatype: 'uint16' },
    { id: 'Unsigned_BA', label: 'Unsigned_BA (16位 / 1 寄存器)', bytes: 2, parseType: 'UINT16', wordOrder: 'BA', datatype: 'uint16' },
    { id: 'Hex', label: 'Hex (16位 / 1 寄存器)', bytes: 2, parseType: 'UINT16', wordOrder: 'AB', datatype: 'uint16' },
    { id: 'Binary', label: 'Binary (16位 / 1 寄存器)', bytes: 2, parseType: 'UINT16', wordOrder: 'AB', datatype: 'uint16' },
    // ===== 4 字节（2 寄存器）=====
    { id: 'Signed_ABCD', label: 'Signed_ABCD (32位 / 2 寄存器)', bytes: 4, parseType: 'INT32', wordOrder: 'ABCD', datatype: 'int32' },
    { id: 'Signed_CDAB', label: 'Signed_CDAB (32位 / 2 寄存器)', bytes: 4, parseType: 'INT32', wordOrder: 'CDAB', datatype: 'int32' },
    { id: 'Signed_BADC', label: 'Signed_BADC (32位 / 2 寄存器)', bytes: 4, parseType: 'INT32', wordOrder: 'BADC', datatype: 'int32' },
    { id: 'Signed_DCBA', label: 'Signed_DCBA (32位 / 2 寄存器)', bytes: 4, parseType: 'INT32', wordOrder: 'DCBA', datatype: 'int32' },
    { id: 'Unsigned_ABCD', label: 'Unsigned_ABCD (32位 / 2 寄存器)', bytes: 4, parseType: 'UINT32', wordOrder: 'ABCD', datatype: 'uint32' },
    { id: 'Unsigned_CDAB', label: 'Unsigned_CDAB (32位 / 2 寄存器)', bytes: 4, parseType: 'UINT32', wordOrder: 'CDAB', datatype: 'uint32' },
    { id: 'Unsigned_BADC', label: 'Unsigned_BADC (32位 / 2 寄存器)', bytes: 4, parseType: 'UINT32', wordOrder: 'BADC', datatype: 'uint32' },
    { id: 'Unsigned_DCBA', label: 'Unsigned_DCBA (32位 / 2 寄存器)', bytes: 4, parseType: 'UINT32', wordOrder: 'DCBA', datatype: 'uint32' },
    { id: 'Float_ABCD', label: 'Float_ABCD (32位 / 2 寄存器)', bytes: 4, parseType: 'FLOAT32', wordOrder: 'ABCD', datatype: 'float32' },
    { id: 'Float_CDAB', label: 'Float_CDAB (32位 / 2 寄存器)', bytes: 4, parseType: 'FLOAT32', wordOrder: 'CDAB', datatype: 'float32' },
    { id: 'Float_BADC', label: 'Float_BADC (32位 / 2 寄存器)', bytes: 4, parseType: 'FLOAT32', wordOrder: 'BADC', datatype: 'float32' },
    { id: 'Float_DCBA', label: 'Float_DCBA (32位 / 2 寄存器)', bytes: 4, parseType: 'FLOAT32', wordOrder: 'DCBA', datatype: 'float32' },
    // ===== 8 字节（4 寄存器）=====
    { id: 'Double_ABCDEFGH', label: 'Double_ABCDEFGH (64位 / 4 寄存器)', bytes: 8, parseType: 'FLOAT64', wordOrder: 'ABCD', datatype: 'float64' },
    { id: 'Double_GHEFCDAB', label: 'Double_GHEFCDAB (64位 / 4 寄存器)', bytes: 8, parseType: 'FLOAT64', wordOrder: 'CDAB', datatype: 'float64' },
    { id: 'Double_BADCFEHG', label: 'Double_BADCFEHG (64位 / 4 寄存器)', bytes: 8, parseType: 'FLOAT64', wordOrder: 'BADC', datatype: 'float64' },
    { id: 'Double_HGFEDCBA', label: 'Double_HGFEDCBA (64位 / 4 寄存器)', bytes: 8, parseType: 'FLOAT64', wordOrder: 'DCBA', datatype: 'float64' }
]

// 按字节数分组的标题（帮助速查表 / 帮助面板共用）
export const FORMAT_GROUP_TITLES = {
    1: '1 字节 / 位段',
    2: '2 字节 / 1 寄存器',
    4: '4 字节 / 2 寄存器',
    8: '8 字节 / 4 寄存器'
}

// 预设 id → 点位 format 字段落盘值（Hex/Binary 用小写语义值，其余 id 即 format 值）
export const presetIdToFormat = (id) => {
    if (!id) return ''
    if (id === 'Hex') return 'hex'
    if (id === 'Binary') return 'binary'
    return id
}

// 2 字节点位 UI 字序为 AB/BA，落盘 word_order 映射为 ABCD/DCBA（wordOrderToBackend），
// 推断时视为等价；空字序按默认 ABCD（各家族的标准大端变体）处理。
const normWordOrder = (wo) => {
    const s = (wo || '').toUpperCase()
    if (s === 'BA') return 'DCBA'
    if (s === '' || s === 'AB') return 'ABCD'
    return s
}

// 点位 → 预设 id 推断。
// 预设描述原始字节解析：优先按 parse_type 匹配（不被缩放后的显示/存储 datatype 反向干扰），
// 无匹配（如 BCD16/STRING 等非预设解析类型）时再按 datatype 回退；
// format 里的 hex/binary/bcd8 是显示语义值，永远最优先。
export const inferPresetId = (p) => {
    if (!p) return null
    const fmt = (p.format || '').toLowerCase()
    if (fmt === 'hex') return 'Hex'
    if (fmt === 'binary') return 'Binary'
    if (fmt === 'bcd8') return 'BCD8'

    const wo = normWordOrder(p.word_order)
    const parseType = (p.parse_type || p.parseType || '').toUpperCase().replace(/_SWAP$/, '')
    const dt = (p.datatype || '').toLowerCase()

    const match = (key, val) => {
        if (!val) return null
        const strict = formatPresets.find(pr => pr[key] === val && normWordOrder(pr.wordOrder) === wo)
        if (strict) return strict.id
        // 字序对不上（如 1 字节位段继承了设备级 4 字节字序）时退回同族第一项（ABCD 默认序）
        return formatPresets.find(pr => pr[key] === val)?.id || null
    }
    return match('parseType', parseType) || match('datatype', dt)
}
