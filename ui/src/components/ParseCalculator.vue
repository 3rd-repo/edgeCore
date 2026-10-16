<template>
  <Teleport to="body">
    <div
      v-if="visible"
      ref="panelRef"
      class="pcalc"
      :class="{ 'is-collapsed': collapsed }"
      :style="panelStyle"
      role="dialog"
      aria-label="解析计算器"
    >
      <!-- 拖拽调整大小：右缘 / 下缘 / 右下角 -->
      <div class="pcalc-resize pcalc-resize--e" @pointerdown="onResizeStart($event, 'e')"></div>
      <div class="pcalc-resize pcalc-resize--s" @pointerdown="onResizeStart($event, 's')"></div>
      <div class="pcalc-resize pcalc-resize--se" title="拖拽调整大小" @pointerdown="onResizeStart($event, 'se')"></div>

      <!-- 标题栏：可拖拽移动 -->
      <div class="pcalc__header" @pointerdown="onDragStart">
        <span class="pcalc__logo" aria-hidden="true">⇄</span>
        <span class="pcalc__title">解析计算器</span>
        <span class="pcalc__subtitle">字节 ⇄ 工程值 全过程演示</span>
        <span class="pcalc__spacer"></span>
        <button class="pcalc__btn" :title="collapsed ? '展开' : '折叠'" @click="collapsed = !collapsed">
          {{ collapsed ? '▣' : '—' }}
        </button>
        <button class="pcalc__btn" title="关闭" @click="emit('update:visible', false)">✕</button>
      </div>

      <div v-show="!collapsed" class="pcalc__body" :style="bodyStyle">
        <!-- 步骤 1：预设与方向 -->
        <div class="pcalc-stage">
          <div class="pcalc-stage__title"><span class="pcalc-no">1</span>格式预设</div>
          <div class="pcalc-row">
            <a-select
              v-model="presetId"
              size="mini"
              allow-search
              class="pcalc-preset-select"
              :options="presetOptions"
              :filter-option="filterPreset"
              popup-container="#pcalc-popup-root"
            />
            <a-radio-group v-model="direction" type="button" size="mini">
              <a-radio value="forward">字节 → 值</a-radio>
              <a-radio value="reverse">值 → 字节</a-radio>
            </a-radio-group>
          </div>
          <div class="pcalc-hint" v-html="presetHint"></div>
          <div class="pcalc-examples">
            <button
              v-for="ex in examples"
              :key="ex.label"
              class="pcalc-example"
              :title="`${ex.presetId} · scale ${ex.scale}`"
              @click="applyExample(ex)"
            >{{ ex.label }}</button>
          </div>
        </div>

        <!-- 步骤 2：输入 -->
        <div class="pcalc-stage">
          <div class="pcalc-stage__title">
            <span class="pcalc-no">2</span>{{ direction === 'forward' ? '线上原始字节（十六进制 / 二进制）' : '期望工程值' }}
          </div>
          <a-input
            v-if="direction === 'forward'"
            v-model="hexInput"
            size="mini"
            class="pcalc-hexinput"
            placeholder="如 01 C8、49 9A 68 16，或 0b 00000000 11101011"
            allow-clear
          />
          <a-input
            v-else
            v-model="engInput"
            size="mini"
            class="pcalc-hexinput"
            :placeholder="engPlaceholder"
            allow-clear
          />
          <div v-if="padNote" class="pcalc-warn">{{ padNote }}</div>
          <div v-if="inputError" class="pcalc-error">{{ inputError }}</div>
          <div v-if="preset.parseType === 'BIT'" class="pcalc-bits">
            <span class="pcalc-bits__label">选择位：</span>
            <button
              v-for="i in 8"
              :key="i"
              class="pcalc-bits__btn"
              :class="{ 'is-on': bitIndex === i - 1 }"
              @click="bitIndex = i - 1"
            >bit{{ i - 1 }}</button>
          </div>
        </div>

        <!-- 步骤 3：字节布局可视化 -->
        <div class="pcalc-stage">
          <div class="pcalc-stage__title"><span class="pcalc-no">3</span>字节布局（悬停查看对应关系）</div>
          <div
            v-for="row in byteRows"
            :key="row.key"
            class="pcalc-byterow"
          >
            <div class="pcalc-byterow__label" :class="{ 'is-value': row.key === 'value' }">{{ row.label }}</div>
            <div class="pcalc-byterow__chips">
              <template v-for="(b, i) in row.bytes" :key="i">
                <span v-if="i && i % 2 === 0" class="pcalc-regdiv" :title="`寄存器 ${i / 2 + 1}`"></span>
                <button
                  class="pcalc-chip"
                  :class="{ 'is-hl': row.hl(i), 'is-reg2': i % 2 === 2, 'is-value': row.key === 'value' }"
                  :title="row.titles[i]"
                  @mouseenter="row.hover && row.hover(i)"
                  @mouseleave="hoverKey = ''"
                >
                  <i class="pcalc-chip__tag">{{ row.tags[i] }}</i>
                  <b>{{ hex2(b) }}</b>
                </button>
              </template>
            </div>
            <div v-if="row.hint" class="pcalc-byterow__hint">{{ row.hint }}</div>
          </div>

          <!-- 全解析模式对照：字节 → 值，同一串字节在所有模式下的取值 -->
          <div v-if="direction === 'forward'" class="pcalc-orders">
            <div class="pcalc-orders__head">
              <span>全解析模式对照</span>
              <span v-if="allParseGroups.length" class="pcalc-orders__note">同一段字节在不同模式下的值 · 点击行切换预设</span>
            </div>
            <div v-if="!allParseGroups.length" class="pcalc-orders__empty">输入字节后展示全解析对照</div>
            <div v-for="(g, gi) in allParseGroups" :key="g.label" class="pcalc-orders__group">
              <div class="pcalc-orders__group-label">{{ g.label }}</div>
              <button
                v-for="r in g.rows"
                :key="g.label + gi + r.presetId"
                class="pcalc-orders__row"
                :class="{ 'is-active': r.presetId === presetId }"
                :title="`切换到 ${r.presetId} 查看完整解析`"
                @click="presetId = r.presetId"
              >
                <span class="pcalc-orders__id">{{ r.id }}</span>
                <span class="pcalc-orders__val">{{ r.value }}</span>
              </button>
            </div>
          </div>

          <!-- 同值全字序对照：值 → 字节的核心应用，与设备报文对照即可反推字序 -->
          <div v-if="direction === 'reverse'" class="pcalc-orders">
            <div class="pcalc-orders__head">
              <span>同值全字序对照</span>
              <span v-if="allOrderGroups.length" class="pcalc-orders__note">点击行切换预设 · 按当前 scale {{ scale ?? 1 }} / offset {{ offset ?? 0 }} 换算</span>
            </div>
            <div v-if="!allOrderGroups.length" class="pcalc-orders__empty">请输入有效工程值</div>
            <div v-for="g in allOrderGroups" :key="g.label" class="pcalc-orders__group">
              <div class="pcalc-orders__group-label">{{ g.label }}</div>
              <button
                v-for="r in g.rows"
                :key="r.presetId"
                class="pcalc-orders__row"
                :class="{ 'is-active': r.presetId === presetId }"
                :title="`切换到 ${r.presetId}`"
                @click="presetId = r.presetId"
              >
                <span class="pcalc-orders__id">{{ r.presetId }}</span>
                <span class="pcalc-orders__wire">{{ formatWireBytes(r.wire) }}</span>
              </button>
            </div>
          </div>
        </div>

        <!-- 步骤 4：解析原始值 -->
        <div class="pcalc-stage">
          <div class="pcalc-stage__title"><span class="pcalc-no">4</span>解析类型原始值</div>
          <div class="pcalc-raw">
            <span class="pcalc-badge">{{ preset.parseType }}</span>
            <b class="pcalc-raw__num">{{ rawText }}</b>
          </div>
          <div v-if="rawAltLines.length" class="pcalc-alt">
            <div v-for="l in rawAltLines" :key="l">{{ l }}</div>
          </div>
        </div>

        <!-- 步骤 5：缩放 -->
        <div class="pcalc-stage">
          <div class="pcalc-stage__title">
            <span class="pcalc-no">5</span>{{ direction === 'forward' ? '缩放为工程值（×scale + offset）' : '反推原始值（(工程值 − offset) ÷ scale）' }}
          </div>
          <div class="pcalc-scale">
            <label>scale <a-input-number v-model="scale" size="mini" :step="0.1" class="pcalc-num" /></label>
            <label>offset <a-input-number v-model="offset" size="mini" :step="0.1" class="pcalc-num" /></label>
            <span v-if="direction === 'forward'" class="pcalc-scale__eq">=</span>
            <b v-if="direction === 'forward'" class="pcalc-eng">{{ engText }}</b>
            <button v-if="direction === 'forward'" class="pcalc-copy" title="复制工程值" @click="copyEng">{{ copied ? '已复制 ✓' : '复制' }}</button>
          </div>
          <div v-if="direction === 'forward' && scaleNote" class="pcalc-hint">{{ scaleNote }}</div>
        </div>

        <!-- 步骤 6：数据类型 -->
        <div class="pcalc-stage pcalc-stage--last">
          <div class="pcalc-stage__title"><span class="pcalc-no">6</span>存储 / 北向数据类型</div>
          <div class="pcalc-hint pcalc-dtnote">
            <span class="pcalc-badge pcalc-badge--dt">{{ preset.datatype }}</span>
            显示、存储与北向输出使用该类型；预设描述的是原始字节解析，改数据类型不会改变解析宽度（如 Signed 解析 int16 后经 scale 0.1 得到小数，用 float32 承载）。
          </div>
        </div>
      </div>
    </div>

    <!-- Select 弹层专用容器：面板 z-index 3000 高于 Arco popup 动态分配的 1001，
         弹层会被面板盖住；挂到这个 z-index 3100 的 fixed 容器里才能浮在面板之上 -->
    <Teleport to="body">
      <div v-if="visible" id="pcalc-popup-root"></div>
    </Teleport>
  </Teleport>
</template>

<script setup>
// 解析计算器：独立浮动组件，演示 Modbus「线上字节 → 字序重排 → 解析类型原始值 → 缩放 → 工程值」全过程。
// 预设数据引用 utils/formatPresets.js（单一事实源），不在此复制预设表。
// 面板支持：标题栏拖拽移动；右缘 / 下缘 / 右下角拖拽调整宽高；Esc 关闭；折叠。
import { computed, ref, watch, nextTick, onUnmounted } from 'vue'
import { formatPresets, FORMAT_GROUP_TITLES } from '@/utils/formatPresets'

const props = defineProps({
  visible: { type: Boolean, default: false }
})
const emit = defineEmits(['update:visible'])

// ---------- 面板状态 / 拖拽移动 ----------
const panelRef = ref(null)
const collapsed = ref(false)
const pos = ref(null) // { x, y } 左上角
const size = ref(null) // { w, h } 面板宽 / 正文最大高

const ensureSize = () => {
  if (!size.value) {
    size.value = {
      w: 448,
      h: Math.max(280, Math.min(600, Math.round(window.innerHeight * 0.72)))
    }
  }
}
const ensurePos = () => {
  if (pos.value) return
  ensureSize()
  pos.value = {
    x: Math.max(8, window.innerWidth - size.value.w - 28),
    y: Math.max(8, Math.min(84, window.innerHeight * 0.12))
  }
}
const clampPos = () => {
  if (!pos.value || !panelRef.value) return
  const el = panelRef.value
  pos.value = {
    x: Math.min(Math.max(8, pos.value.x), Math.max(8, window.innerWidth - el.offsetWidth - 8)),
    y: Math.min(Math.max(8, pos.value.y), Math.max(8, window.innerHeight - 48))
  }
}
const panelStyle = computed(() => {
  const s = { ...(pos.value ? { left: `${pos.value.x}px`, top: `${pos.value.y}px` } : {}) }
  if (size.value) s.width = `${size.value.w}px`
  return s
})
const bodyStyle = computed(() => (size.value ? { maxHeight: `${size.value.h}px` } : {}))

watch(() => props.visible, async (v) => {
  if (v) {
    await nextTick()
    ensureSize()
    ensurePos()
    clampPos()
  }
})

let dragCtx = null
const onDragStart = (e) => {
  if (e.target.closest('button')) return
  ensurePos()
  dragCtx = { sx: e.clientX, sy: e.clientY, ox: pos.value.x, oy: pos.value.y }
  e.currentTarget.setPointerCapture?.(e.pointerId)
  window.addEventListener('pointermove', onDragMove)
  window.addEventListener('pointerup', onDragEnd)
}
const onDragMove = (e) => {
  if (!dragCtx) return
  pos.value = { x: dragCtx.ox + (e.clientX - dragCtx.sx), y: dragCtx.oy + (e.clientY - dragCtx.sy) }
  clampPos()
}
const onDragEnd = () => {
  dragCtx = null
  window.removeEventListener('pointermove', onDragMove)
  window.removeEventListener('pointerup', onDragEnd)
}

// ---------- 拖拽调整大小（e = 宽，s = 高，se = 双向） ----------
let resizeCtx = null
const onResizeStart = (e, dir) => {
  e.preventDefault()
  e.stopPropagation()
  ensureSize()
  ensurePos()
  resizeCtx = { dir, sx: e.clientX, sy: e.clientY, sw: size.value.w, sh: size.value.h }
  e.currentTarget.setPointerCapture?.(e.pointerId)
  window.addEventListener('pointermove', onResizeMove)
  window.addEventListener('pointerup', onResizeEnd)
}
const onResizeMove = (e) => {
  if (!resizeCtx) return
  const { dir, sx, sy, sw, sh } = resizeCtx
  const maxW = Math.min(860, window.innerWidth - 16)
  const maxH = Math.max(260, window.innerHeight - 60)
  if (dir.includes('e')) size.value = { ...size.value, w: Math.min(maxW, Math.max(384, sw + e.clientX - sx)) }
  if (dir.includes('s')) size.value = { ...size.value, h: Math.min(maxH, Math.max(260, sh + e.clientY - sy)) }
  clampPos()
}
const onResizeEnd = () => {
  resizeCtx = null
  window.removeEventListener('pointermove', onResizeMove)
  window.removeEventListener('pointerup', onResizeEnd)
}

const onKeydown = (e) => { if (e.key === 'Escape' && props.visible) emit('update:visible', false) }
window.addEventListener('keydown', onKeydown)
onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('pointermove', onDragMove)
  window.removeEventListener('pointerup', onDragEnd)
  window.removeEventListener('pointermove', onResizeMove)
  window.removeEventListener('pointerup', onResizeEnd)
})

// ---------- 预设 ----------
const presetId = ref('Signed_AB')
const preset = computed(() => formatPresets.find(p => p.id === presetId.value) || formatPresets[0])
const direction = ref('forward')
const scale = ref(0.1)
const offset = ref(0)
const bitIndex = ref(0)

// 预设下拉：options prop 分组格式（{ isGroup, label, options }），Arco 2.58 官方支持；
// 插槽 a-optgroup 自注册在虚拟滚动下会漏项，弃用。
const presetOptions = computed(() => [1, 2, 4, 8].map(b => ({
  isGroup: true,
  label: FORMAT_GROUP_TITLES[b],
  options: formatPresets.filter(p => p.bytes === b).map(p => ({ label: p.label, value: p.id }))
})))
const filterPreset = (input, option) => {
  const id = String(option.value || '').toLowerCase()
  const label = String(option.label || '').toLowerCase()
  const q = input.toLowerCase()
  return id.includes(q) || label.includes(q)
}

// 预设命名中的字节序列：1 字节无字序；8 字节取 id 展开的 8 字母；其余用 wordOrder（AB/ABCD/CDAB/BADC/DCBA）
const suffix = computed(() => {
  const p = preset.value
  if (p.bytes === 1) return 'A'
  if (p.bytes === 8) return p.id.split('_')[1]
  return p.wordOrder
})
const letters = computed(() => 'ABCDEFGH'.slice(0, preset.value.bytes))
const isIdentityOrder = computed(() => suffix.value === letters.value)

const presetHint = computed(() => {
  const p = preset.value
  const s = suffix.value
  if (p.bytes === 1) return `${p.id}：单字节位段，无字序概念。`
  const wireDesc = s.split('').join(' ')
  return `${p.id}：线上字节依次为 ${wireDesc}${isIdentityOrder.value ? '（标准大端，无需重排）' : '，需重排为 ' + letters.value.split('').join(' ') + ' 后再解析'}。`
})

// ---------- 字节解析 / 编码 ----------
const hex2 = (b) => (b || 0).toString(16).toUpperCase().padStart(2, '0')
// 线上字节文本：两位一组（寄存器）之间用双空格分隔
const formatWireBytes = (bytes) =>
  bytes.map((b, i) => (i && i % 2 === 0 ? '  ' : i ? ' ' : '') + hex2(b)).join('')

const parseHexInput = (s) => {
  const hex = (s || '').replace(/[^0-9a-fA-F]/g, '')
  const out = []
  for (let i = 0; i + 1 < hex.length; i += 2) out.push(parseInt(hex.slice(i, i + 2), 16))
  return out
}

// 字节输入：支持 0b 前缀的二进制（如 0b 00000000 11101011），其余按十六进制
const isBinaryInput = (s) => /^0b[01\s_,]+$/i.test((s || '').trim())
const parseByteInput = (s) => {
  if (!isBinaryInput(s)) return parseHexInput(s)
  const body = (s || '').replace(/^0b/i, '')
  const bits = body.replace(/[^01]/g, '')
  if (!bits) return []
  const pad = (8 - (bits.length % 8)) % 8
  const full = '0'.repeat(pad) + bits
  const out = []
  for (let i = 0; i + 8 <= full.length; i += 8) out.push(parseInt(full.slice(i, i + 8), 2))
  return out
}

const hexInput = ref('00 EB')
const engInput = ref('23.5')
const engPlaceholder = computed(() =>
  preset.value.parseType.startsWith('FLOAT') ? '如 1234.56' : `如 235（原始值 = 工程值 ÷ scale）`)

// 正向：线上字节（补齐/截断到预设宽度）
const wireBytesForward = computed(() => {
  const n = preset.value.bytes
  const raw = parseByteInput(hexInput.value)
  const out = raw.slice(0, n)
  while (out.length < n) out.push(0)
  return out
})
const padNote = computed(() => {
  if (direction.value !== 'forward') return ''
  const cnt = parseByteInput(hexInput.value).length
  if (cnt === preset.value.bytes) return ''
  if (cnt === 0) return `等待输入 ${preset.value.bytes} 字节（当前按 0x00 处理）`
  return cnt < preset.value.bytes
    ? `输入不足 ${preset.value.bytes} 字节，高位已按 0x00 补齐`
    : `输入超出 ${preset.value.bytes} 字节，仅保留前 ${preset.value.bytes} 字节`
})

// 正向重排：valueBytes[letterIdx(suffix[k])] = wire[k]；反向为逆映射 wire[letterIdx(suffix[k])] = value[k]
const permuteForward = (wire) => {
  const out = new Array(wire.length).fill(0)
  suffix.value.split('').forEach((ch, k) => { out[letters.value.indexOf(ch)] = wire[k] })
  return out
}
const permuteReverse = (value) => {
  const out = new Array(value.length).fill(0)
  suffix.value.split('').forEach((ch, k) => { out[k] = value[letters.value.indexOf(ch)] })
  return out
}

const fmtFloat = (v) => (Number.isInteger(v) ? String(v) : String(parseFloat(v.toPrecision(8))))

// 正向：原始解析值
const forwardRaw = computed(() => {
  const vb = permuteForward(wireBytesForward.value)
  const buf = new ArrayBuffer(8)
  const dv = new DataView(buf)
  vb.forEach((b, i) => dv.setUint8(i, b))
  switch (preset.value.parseType) {
    case 'INT16': return dv.getInt16(0, false)
    case 'UINT16': return dv.getUint16(0, false)
    case 'INT32': return dv.getInt32(0, false)
    case 'UINT32': return dv.getUint32(0, false)
    case 'FLOAT32': return dv.getFloat32(0, false)
    case 'FLOAT64': return dv.getFloat64(0, false)
    case 'INT8': return dv.getInt8(0)
    case 'UINT8': return dv.getUint8(0)
    case 'BIT': return (vb[0] >> bitIndex.value) & 1
    case 'BCD8': { const b = vb[0]; return (b >> 4) * 10 + (b & 0x0f) }
    default: return null
  }
})

// 反向：工程值 → 原始值 → 标准字节 → 线上字节
const inputError = computed(() => {
  if (direction.value !== 'reverse') return ''
  const v = parseFloat(engInput.value)
  if (engInput.value === '' || Number.isNaN(v)) return '请输入有效数值'
  const pt = preset.value.parseType
  const raw = (v - (offset.value || 0)) / (scale.value || 1)
  if (pt === 'INT16' && (raw < -32768 || raw > 32767)) return `超出 INT16 范围（-32768 ~ 32767）：${fmtFloat(raw)}`
  if (pt === 'UINT16' && (raw < 0 || raw > 65535)) return '超出 UINT16 范围（0 ~ 65535）'
  if (pt === 'BIT' && raw !== 0 && raw !== 1) return 'BIT 需 0 或 1（非零按 1 处理时请直接输入 1）'
  if (pt === 'INT8' && (raw < -128 || raw > 127)) return '超出 INT8 范围（-128 ~ 127）'
  if (pt === 'UINT8' && (raw < 0 || raw > 255)) return '超出 UINT8 范围（0 ~ 255）'
  if (pt === 'INT32' && (raw < -2147483648 || raw > 2147483647)) return `超出 INT32 范围（-2147483648 ~ 2147483647）`
  if (pt === 'UINT32' && (raw < 0 || raw > 4294967295)) return '超出 UINT32 范围（0 ~ 4294967295）'
  if (pt === 'BCD8' && (raw < 0 || raw > 99 || !Number.isInteger(raw))) return 'BCD8 需 0 ~ 99 的整数'
  return ''
})

const reverseBytes = computed(() => {
  // 返回 { valueBytes, wireBytes }；出错时返回 null
  if (direction.value !== 'reverse' || inputError.value) return null
  const pt = preset.value.parseType
  let raw
  const v = parseFloat(engInput.value)
  if (pt === 'FLOAT32' || pt === 'FLOAT64') raw = (v - (offset.value || 0)) / (scale.value || 1)
  else raw = Math.round((v - (offset.value || 0)) / (scale.value || 1))

  const buf = new ArrayBuffer(8)
  const dv = new DataView(buf)
  const n = preset.value.bytes
  let valueBytes = new Array(n).fill(0)
  try {
    switch (pt) {
      case 'INT16': dv.setInt16(0, raw, false); break
      case 'UINT16': dv.setUint16(0, raw, false); break
      case 'INT32': dv.setInt32(0, raw, false); break
      case 'UINT32': dv.setUint32(0, raw, false); break
      case 'FLOAT32': dv.setFloat32(0, raw, false); break
      case 'FLOAT64': dv.setFloat64(0, raw, false); break
      case 'INT8': dv.setInt8(0, raw); break
      case 'UINT8': dv.setUint8(0, raw); break
      case 'BIT': dv.setUint8(0, raw ? (1 << bitIndex.value) : 0); break
      case 'BCD8': dv.setUint8(0, ((Math.floor(raw / 10)) << 4) | (raw % 10)); break
      default: return null
    }
  } catch { return null }
  valueBytes = Array.from(new Uint8Array(buf, 0, n))
  return { valueBytes, wireBytes: permuteReverse(valueBytes) }
})

// ---------- 同值全字序对照（值 → 字节的核心应用）----------
// 输入一个工程值，铺开所有字节序下的线上字节，与设备报文对照即可反推设备用的字序；
// 点击任意行直接切换到该预设。
const ORDER_FAMILIES = [
  { bytes: 2, label: '2 字节 / 1 寄存器', parseTypes: ['INT16', 'UINT16'], orders: ['AB', 'BA'] },
  { bytes: 4, label: '4 字节 / 2 寄存器', parseTypes: ['INT32', 'UINT32', 'FLOAT32'], orders: ['ABCD', 'CDAB', 'BADC', 'DCBA'] },
  { bytes: 8, label: '8 字节 / 4 寄存器', parseTypes: ['FLOAT64'], orders: ['ABCDEFGH', 'GHEFCDAB', 'BADCFEHG', 'HGFEDCBA'] }
]
const PRESET_BY_PT_ORDER = {
  'INT16|AB': 'Signed_AB', 'INT16|BA': 'Signed_BA',
  'UINT16|AB': 'Unsigned_AB', 'UINT16|BA': 'Unsigned_BA',
  'INT32|ABCD': 'Signed_ABCD', 'INT32|CDAB': 'Signed_CDAB', 'INT32|BADC': 'Signed_BADC', 'INT32|DCBA': 'Signed_DCBA',
  'UINT32|ABCD': 'Unsigned_ABCD', 'UINT32|CDAB': 'Unsigned_CDAB', 'UINT32|BADC': 'Unsigned_BADC', 'UINT32|DCBA': 'Unsigned_DCBA',
  'FLOAT32|ABCD': 'Float_ABCD', 'FLOAT32|CDAB': 'Float_CDAB', 'FLOAT32|BADC': 'Float_BADC', 'FLOAT32|DCBA': 'Float_DCBA',
  'FLOAT64|ABCDEFGH': 'Double_ABCDEFGH', 'FLOAT64|GHEFCDAB': 'Double_GHEFCDAB',
  'FLOAT64|BADCFEHG': 'Double_BADCFEHG', 'FLOAT64|HGFEDCBA': 'Double_HGFEDCBA'
}
const rawInRange = (pt, r) => {
  if (r === null || !Number.isFinite(r)) return false
  switch (pt) {
    case 'INT16': return r >= -32768 && r <= 32767
    case 'UINT16': return r >= 0 && r <= 65535
    case 'INT32': return r >= -2147483648 && r <= 2147483647
    case 'UINT32': return r >= 0 && r <= 4294967295
    default: return true // FLOAT32 / FLOAT64
  }
}
const encodeValueBytesTo = (pt, raw, n) => {
  const buf = new ArrayBuffer(8)
  const dv = new DataView(buf)
  try {
    switch (pt) {
      case 'INT16': dv.setInt16(0, raw, false); break
      case 'UINT16': dv.setUint16(0, raw, false); break
      case 'INT32': dv.setInt32(0, raw, false); break
      case 'UINT32': dv.setUint32(0, raw, false); break
      case 'FLOAT32': dv.setFloat32(0, raw, false); break
      case 'FLOAT64': dv.setFloat64(0, raw, false); break
      default: return null
    }
  } catch { return null }
  return Array.from(new Uint8Array(buf, 0, n))
}
const permuteWireTo = (valueBytes, suf) => {
  const letters = 'ABCDEFGH'.slice(0, valueBytes.length)
  const out = new Array(valueBytes.length).fill(0)
  suf.split('').forEach((ch, k) => { out[letters.indexOf(ch)] = valueBytes[k] })
  return out
}
const allOrderGroups = computed(() => {
  if (direction.value !== 'reverse') return []
  const v = parseFloat(engInput.value)
  if (engInput.value === '' || Number.isNaN(v)) return []
  // 不因当前预设的范围错误而整块隐藏：按解析类型逐行过滤（INT16 超范围时 INT32/FLOAT 照常展示）
  const frac = (v - (offset.value || 0)) / (scale.value || 1)
  const rawFor = (fpt) => fpt.startsWith('FLOAT') ? frac : Math.round(frac)

  return ORDER_FAMILIES.map(fam => {
    const rows = []
    for (const fpt of fam.parseTypes) {
      const raw = rawFor(fpt)
      if (!rawInRange(fpt, raw)) continue
      const vb = encodeValueBytesTo(fpt, raw, fam.bytes)
      if (!vb) continue
      for (const suf of fam.orders) {
        rows.push({ presetId: PRESET_BY_PT_ORDER[`${fpt}|${suf}`], pt: fpt, wire: permuteWireTo(vb, suf) })
      }
    }
    return { label: fam.label, rows }
  }).filter(g => g.rows.length)
})

// ---------- 字节 → 值：全解析模式对照 ----------
// 同一段线上字节，在所有解析模式下的取值一屏铺开；点击行切换到该预设查看完整管线。
const permuteForwardTo = (wire, suf) => {
  const letters = 'ABCDEFGH'.slice(0, wire.length)
  const out = new Array(wire.length).fill(0)
  suf.split('').forEach((ch, k) => { out[letters.indexOf(ch)] = wire[k] })
  return out
}
const parseBE = (vb, pt) => {
  const dv = new DataView(new ArrayBuffer(8))
  vb.forEach((b, i) => dv.setUint8(i, b))
  switch (pt) {
    case 'INT16': return dv.getInt16(0, false)
    case 'UINT16': return dv.getUint16(0, false)
    case 'INT32': return dv.getInt32(0, false)
    case 'UINT32': return dv.getUint32(0, false)
    case 'FLOAT32': return dv.getFloat32(0, false)
    case 'FLOAT64': return dv.getFloat64(0, false)
    case 'INT8': return dv.getInt8(0)
    case 'UINT8': return dv.getUint8(0)
    default: return null
  }
}
const allParseGroups = computed(() => {
  if (direction.value !== 'forward') return []
  const bytes = parseByteInput(hexInput.value)
  if (!bytes.length) return []
  const n = bytes.length
  const groups = []

  // 8 位：首字节
  const b0 = bytes[0]
  const bcd = ((b0 >> 4) > 9 || (b0 & 0x0f) > 9) ? null : (b0 >> 4) * 10 + (b0 & 0x0f)
  groups.push({
    label: '8 位 / 单字节（首字节）',
    rows: [
      { presetId: 'UINT8', id: 'UINT8', value: String(b0) },
      { presetId: 'INT8', id: 'INT8', value: String(b0 >= 128 ? b0 - 256 : b0) },
      { presetId: 'BCD8', id: 'BCD8', value: bcd === null ? '无效 BCD' : String(bcd) }
    ]
  })

  // 16 位：逐寄存器
  const regCount = Math.floor(n / 2)
  for (let k = 0; k < regCount; k++) {
    const reg = [bytes[k * 2], bytes[k * 2 + 1]]
    const swapped = [reg[1], reg[0]]
    const label = regCount > 1 ? `16 位 / 1 寄存器（寄存器 ${k + 1}）` : '16 位 / 1 寄存器'
    groups.push({
      label,
      rows: [
        { presetId: 'Signed_AB', id: 'Signed_AB', value: String(parseBE(reg, 'INT16')) },
        { presetId: 'Signed_BA', id: 'Signed_BA', value: String(parseBE(swapped, 'INT16')) },
        { presetId: 'Unsigned_AB', id: 'Unsigned_AB', value: String(parseBE(reg, 'UINT16')) },
        { presetId: 'Unsigned_BA', id: 'Unsigned_BA', value: String(parseBE(swapped, 'UINT16')) }
      ]
    })
  }

  // 32 位：4 种字序 × 整型/浮点
  if (n >= 4) {
    const w4 = bytes.slice(0, 4)
    const orders4 = [['ABCD', w4], ['CDAB', [w4[2], w4[3], w4[0], w4[1]]], ['BADC', [w4[1], w4[0], w4[3], w4[2]]], ['DCBA', [w4[3], w4[2], w4[1], w4[0]]]]
    const rows = []
    for (const [suf, vb] of orders4) {
      rows.push(
        { presetId: `Signed_${suf}`, id: `Signed_${suf}`, value: String(parseBE(vb, 'INT32')) },
        { presetId: `Unsigned_${suf}`, id: `Unsigned_${suf}`, value: String(parseBE(vb, 'UINT32')) },
        { presetId: `Float_${suf}`, id: `Float_${suf}`, value: fmtFloat(parseBE(vb, 'FLOAT32')) }
      )
    }
    groups.push({ label: '32 位 / 2 寄存器', rows })
  }

  // 64 位：双精度
  if (n >= 8) {
    const w8 = bytes.slice(0, 8)
    const orders8 = [['ABCDEFGH', w8], ['GHEFCDAB', [w8[6], w8[7], w8[4], w8[5], w8[2], w8[3], w8[0], w8[1]]], ['BADCFEHG', [w8[1], w8[0], w8[3], w8[2], w8[5], w8[4], w8[7], w8[6]]], ['HGFEDCBA', [...w8].reverse()]]
    groups.push({
      label: '64 位 / 4 寄存器（双精度）',
      rows: orders8.map(([suf, vb]) => ({ presetId: `Double_${suf}`, id: `Double_${suf}`, value: fmtFloat(parseBE(vb, 'FLOAT64')) }))
    })
  }
  return groups
})

// ---------- 展示 ----------
const rawText = computed(() => {
  if (direction.value === 'reverse') {
    if (inputError.value) return '—'
    const pt = preset.value.parseType
    if (pt === 'FLOAT32' || pt === 'FLOAT64') {
      const v = (parseFloat(engInput.value) - (offset.value || 0)) / (scale.value || 1)
      return fmtFloat(v)
    }
    return String(Math.round((parseFloat(engInput.value) - (offset.value || 0)) / (scale.value || 1)))
  }
  if (forwardRaw.value === null) return '—'
  const pt = preset.value.parseType
  if (presetId.value === 'Hex') return `0x${wireBytesForward.value.map(hex2).join(' ')}`
  if (presetId.value === 'Binary') return '0b' + wireBytesForward.value.map(b => b.toString(2).padStart(8, '0')).join(' ')
  if (pt === 'FLOAT32' || pt === 'FLOAT64') return fmtFloat(forwardRaw.value)
  return String(forwardRaw.value)
})

const rawAltLines = computed(() => {
  const lines = []
  if (direction.value === 'reverse') return lines
  const pt = preset.value.parseType
  if (pt === 'FLOAT32' || pt === 'FLOAT64' || presetId.value === 'Hex' || presetId.value === 'Binary') {
    lines.push(`十进制：${forwardRaw.value === null ? '—' : fmtFloat(forwardRaw.value)}`)
  }
  if (pt !== 'BIT' && presetId.value !== 'Hex' && presetId.value !== 'Binary') {
    lines.push(`十六进制：0x ${wireBytesForward.value.map(hex2).join(' ')}`)
    if (preset.value.bytes <= 2) lines.push(`二进制：0b ${wireBytesForward.value.map(b => b.toString(2).padStart(8, '0')).join(' ')}`)
  }
  if (pt === 'BIT') {
    const byte = permuteForward(wireBytesForward.value)[0]
    lines.push(`字节 0b ${byte.toString(2).padStart(8, '0')} → 取 bit${bitIndex.value} = ${(byte >> bitIndex.value) & 1}`)
  }
  if (preset.value.bytes === 1 && pt !== 'BIT') lines.push('单字节点位：bit 段解析，无字序。')
  return lines
})

const engText = computed(() => {
  if (forwardRaw.value === null) return '—'
  const v = forwardRaw.value * (scale.value ?? 1) + (offset.value ?? 0)
  return fmtFloat(v)
})
const scaleNote = computed(() => {
  const pt = preset.value.parseType
  if (pt.startsWith('FLOAT')) return 'float 原始值本身已是小数，scale 常为 1；float32 有效数字约 7 位。'
  if (scale.value && scale.value !== 1) return `整数原始值 × ${scale.value} 产生小数，工程值建议用 float32 承载。`
  return ''
})

// 字节行（正向：线上 → 标准；反向：标准 → 线上）
const hoverKey = ref('')
const byteRows = computed(() => {
  const n = preset.value.bytes
  const identity = isIdentityOrder.value
  const wire = direction.value === 'forward'
    ? wireBytesForward.value
    : (reverseBytes.value?.wireBytes || new Array(n).fill(0))
  const value = direction.value === 'forward'
    ? permuteForward(wireBytesForward.value)
    : (reverseBytes.value?.valueBytes || new Array(n).fill(0))
  const wireTags = suffix.value.split('')
  const valueTags = letters.value.split('')
  const suffixIdx = (ch) => suffix.value.indexOf(ch)
  // 交叉高亮：悬停线上第 i 字节 → 对应逻辑位 letters[suffix[i]] 在标准大端的下标；
  // 悬停标准大端第 i 字节 → 对应逻辑位 letters[i] 在线上的下标 suffix.indexOf(letters[i])
  const wireHl = (i) => hoverKey.value === `w${i}` || (hoverKey.value.startsWith('v') && suffixIdx(letters.value[+hoverKey.value.slice(1)]) === i)
  const valueHl = (i) => hoverKey.value === `v${i}` || (hoverKey.value.startsWith('w') && letters.value.indexOf(suffix.value[+hoverKey.value.slice(1)]) === i)
  const wireTitles = wireTags.map((ch, i) => `线上第 ${i + 1} 字节 · 逻辑位 ${ch}`)
  const valueTitles = valueTags.map((ch, i) => `标准大端第 ${i + 1} 字节 · 来自线上第 ${suffixIdx(ch) + 1} 字节`)
  const regHint = n > 1 ? `共 ${n / 2} 个寄存器，竖线分隔寄存器边界` : ''

  const wireRow = {
    key: 'wire',
    label: direction.value === 'forward' ? '线上字节（寄存器传输顺序）' : '线上字节（设备实际发送）',
    bytes: wire, tags: wireTags, titles: wireTitles, hl: wireHl,
    hover: (i) => { hoverKey.value = `w${i}` },
    hint: regHint
  }
  const valueRow = {
    key: 'value',
    label: identity ? '标准大端（与线上一致，无需重排）' : '重排后 · 标准大端 ' + valueTags.join(''),
    bytes: value, tags: valueTags, titles: valueTitles, hl: valueHl,
    hover: (i) => { hoverKey.value = `v${i}` },
    hint: identity ? '' : `按 ${preset.value.id.includes('_') && n === 8 ? preset.value.id.split('_')[1] : preset.value.wordOrder} 重排：悬停任意字节查看对应关系`
  }
  return direction.value === 'forward' ? [wireRow, valueRow] : [valueRow, wireRow]
})

// ---------- 示例 ----------
const examples = [
  { label: '帧解析 00 EB → 23.5', presetId: 'Signed_AB', scale: 0.1, offset: 0, forwardHex: '00 EB' },
  { label: '温度 23.5℃', presetId: 'Signed_AB', scale: 0.1, offset: 0, eng: 23.5 },
  { label: '湿度 45.6%', presetId: 'Unsigned_AB', scale: 0.1, offset: 0, eng: 45.6 },
  { label: '32位累计量', presetId: 'Unsigned_ABCD', scale: 1, offset: 0, eng: 305419896 },
  { label: '电能 1234.56', presetId: 'Float_ABCD', scale: 1, offset: 0, eng: 1234.56 },
  { label: '电能·字交换', presetId: 'Float_CDAB', scale: 1, offset: 0, eng: 1234.56 },
  { label: '双精度 8888.5', presetId: 'Double_ABCDEFGH', scale: 1, offset: 0, eng: 8888.5 },
  { label: '开关量 bit3', presetId: 'BIT', scale: 1, offset: 0, eng: 1, bitIndex: 3 }
]
const applyExample = (ex) => {
  presetId.value = ex.presetId
  scale.value = ex.scale
  offset.value = ex.offset || 0
  bitIndex.value = ex.bitIndex || 0
  if (ex.forwardHex) {
    direction.value = 'forward'
    hexInput.value = ex.forwardHex
  } else {
    direction.value = 'reverse'
    engInput.value = String(ex.eng)
  }
}

const copied = ref(false)
let copyTimer = null
const copyEng = async () => {
  try {
    await navigator.clipboard.writeText(engText.value)
  } catch {
    const ta = document.createElement('textarea')
    ta.value = engText.value
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    ta.remove()
  }
  copied.value = true
  clearTimeout(copyTimer)
  copyTimer = setTimeout(() => { copied.value = false }, 1200)
}
</script>

<style scoped>
.pcalc {
  position: fixed;
  z-index: 3000;
  width: 448px;
  max-width: calc(100vw - 16px);
  background: var(--color-bg-2, #fff);
  border: 1px solid var(--color-border-2, #e5e6eb);
  border-radius: 12px;
  box-shadow:
    0 1px 2px rgba(0, 0, 0, 0.06),
    0 12px 40px rgba(0, 0, 0, 0.14);
  overflow: hidden;
  user-select: none;
}

/* ---------- 标题栏 ---------- */
.pcalc__header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  background:
    linear-gradient(135deg, rgba(var(--primary-6, 22, 93, 255), 0.10), transparent 55%),
    var(--color-bg-2, #fff);
  border-bottom: 1px solid var(--color-border-2, #e5e6eb);
  cursor: grab;
  touch-action: none;
}
.pcalc__header:active { cursor: grabbing; }
.pcalc__logo {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 7px;
  background: linear-gradient(135deg, rgb(var(--primary-6, 22, 93, 255)), rgb(var(--primary-5, 40, 125, 255)));
  color: #fff;
  font-size: 13px;
  font-weight: 700;
  box-shadow: 0 2px 6px rgba(var(--primary-6, 22, 93, 255), 0.35);
  flex: none;
}
.pcalc__title { font-weight: 600; font-size: 13px; color: var(--color-text-1, #1d2129); white-space: nowrap; }
.pcalc__subtitle { font-size: 11px; color: var(--color-text-3, #86909c); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.pcalc__spacer { flex: 1; }
.pcalc__btn {
  border: none; background: transparent; cursor: pointer;
  color: var(--color-text-3, #86909c); font-size: 13px; line-height: 1;
  padding: 3px 6px; border-radius: 5px; flex: none;
}
.pcalc__btn:hover { background: var(--color-fill-3, #e5e6eb); color: var(--color-text-1, #1d2129); }

/* ---------- 正文：步骤时间线 ---------- */
.pcalc__body {
  padding: 4px 14px 12px;
  max-height: min(72vh, 640px);
  overflow-y: auto;
  overscroll-behavior: contain;
  user-select: text;
}
.pcalc-stage {
  position: relative;
  padding: 10px 0 12px 30px;
}
.pcalc-stage:not(.pcalc-stage--last) { border-bottom: 1px dashed var(--color-border-1, #f0f1f3); }
/* 时间线连接线 */
.pcalc-stage:not(.pcalc-stage--last)::before {
  content: '';
  position: absolute;
  left: 12px;
  top: 32px;
  bottom: -2px;
  width: 0;
  border-left: 1px dashed var(--color-border-2, #e5e6eb);
}
.pcalc-stage__title {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 11px;
  font-weight: 600;
  color: var(--color-text-2, #4e5969);
  margin-bottom: 8px;
  letter-spacing: 0.3px;
}
.pcalc-no {
  position: absolute;
  left: 0;
  top: 9px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: var(--color-fill-2, #f2f3f5);
  border: 1px solid var(--color-border-2, #e5e6eb);
  color: rgb(var(--primary-6, 22, 93, 255));
  font-size: 12px;
  font-weight: 700;
  flex: none;
}

.pcalc-row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.pcalc-preset-select { width: 224px; }

.pcalc-hint { font-size: 11px; color: var(--color-text-3, #86909c); line-height: 1.7; margin-top: 6px; }
.pcalc-warn { font-size: 11px; color: rgb(var(--orange-6, 255, 125, 0)); margin-top: 4px; }
.pcalc-error { font-size: 11px; color: rgb(var(--red-6, 245, 63, 63)); margin-top: 4px; }

/* 示例 pill */
.pcalc-examples { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 8px; }
.pcalc-example {
  border: 1px solid var(--color-border-2, #e5e6eb);
  background: var(--color-fill-1, #f7f8fa);
  color: var(--color-text-2, #4e5969);
  font-size: 11px; padding: 2px 9px; border-radius: 999px; cursor: pointer;
  transition: all 0.15s;
}
.pcalc-example:hover {
  border-color: rgb(var(--primary-6, 22, 93, 255));
  color: rgb(var(--primary-6, 22, 93, 255));
  background: var(--color-primary-light-1, #e8f3ff);
  transform: translateY(-1px);
}
.pcalc-example:active { transform: translateY(0); }

/* ---------- 字节 chips ---------- */
.pcalc-byterow { margin-bottom: 10px; }
.pcalc-byterow:last-child { margin-bottom: 0; }
.pcalc-byterow__label {
  font-size: 11px; color: var(--color-text-3, #86909c); margin-bottom: 5px;
  font-weight: 600; letter-spacing: 0.2px;
}
.pcalc-byterow__label.is-value { color: rgb(var(--primary-6, 22, 93, 255)); }
.pcalc-byterow__chips { display: flex; align-items: center; flex-wrap: wrap; gap: 4px; }
.pcalc-regdiv {
  width: 1px; height: 28px;
  background: var(--color-border-3, #c9cdd4);
  margin: 0 4px; border-radius: 1px;
}
.pcalc-chip {
  position: relative;
  display: inline-flex; flex-direction: column; align-items: center;
  min-width: 36px; padding: 4px 5px 3px;
  border: 1px solid var(--color-border-2, #e5e6eb);
  border-radius: 7px;
  background: var(--color-fill-1, #f7f8fa);
  cursor: default;
  transition: transform 0.12s, box-shadow 0.12s, border-color 0.12s, background 0.12s;
}
.pcalc-chip.is-reg2 { background: var(--color-fill-2, #f2f3f5); }
.pcalc-chip.is-value {
  border-color: var(--color-primary-light-2, #bedaff);
  background: var(--color-primary-light-1, #e8f3ff);
}
.pcalc-chip.is-value .pcalc-chip__tag { color: rgb(var(--primary-6, 22, 93, 255)); }
.pcalc-chip.is-hl {
  border-color: rgb(var(--primary-6, 22, 93, 255));
  background: var(--color-primary-light-1, #e8f3ff);
  transform: translateY(-2px);
  box-shadow: 0 3px 8px rgba(var(--primary-6, 22, 93, 255), 0.25);
}
.pcalc-chip__tag {
  font-style: normal; font-size: 9px; line-height: 1;
  color: var(--color-text-3, #86909c); margin-bottom: 3px;
  font-family: ui-monospace, Consolas, monospace;
}
.pcalc-chip b {
  font-size: 13px; font-family: ui-monospace, Consolas, monospace;
  color: var(--color-text-1, #1d2129); letter-spacing: 0.5px;
}
.pcalc-byterow__hint { font-size: 10px; color: var(--color-text-4, #c9cdd4); margin-top: 4px; }

/* ---------- 原始值 / 缩放 ---------- */
.pcalc-raw {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  padding: 7px 10px;
  border-radius: 8px;
  background: var(--color-fill-1, #f7f8fa);
  border: 1px solid var(--color-border-1, #f0f1f3);
}
.pcalc-raw__num {
  font-family: ui-monospace, Consolas, monospace;
  font-size: 18px; color: var(--color-text-1, #1d2129);
  word-break: break-all;
}
.pcalc-badge {
  display: inline-block; font-size: 10px; font-weight: 600;
  padding: 1px 8px; border-radius: 999px;
  background: var(--color-fill-2, #f2f3f5);
  color: var(--color-text-2, #4e5969);
  border: 1px solid var(--color-border-2, #e5e6eb);
  white-space: nowrap;
}
.pcalc-badge--dt {
  background: var(--color-primary-light-1, #e8f3ff);
  color: rgb(var(--primary-6, 22, 93, 255));
  border-color: var(--color-primary-light-2, #bedaff);
  margin-right: 6px;
}
.pcalc-alt {
  font-size: 11px; color: var(--color-text-3, #86909c);
  font-family: ui-monospace, Consolas, monospace;
  line-height: 1.8; margin-top: 6px;
}
.pcalc-scale { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; font-size: 11px; color: var(--color-text-2, #4e5969); }
.pcalc-scale label { display: inline-flex; align-items: center; gap: 4px; }
.pcalc-num { width: 88px; }
.pcalc-scale__eq { font-size: 14px; font-weight: 700; color: var(--color-text-2, #4e5969); }
.pcalc-eng {
  font-family: ui-monospace, Consolas, monospace;
  font-size: 17px; color: rgb(var(--green-6, 0, 180, 42));
}
.pcalc-copy {
  border: 1px solid var(--color-border-2, #e5e6eb);
  background: var(--color-bg-2, #fff);
  color: var(--color-text-2, #4e5969);
  font-size: 11px; padding: 2px 9px; border-radius: 5px; cursor: pointer;
  transition: all 0.15s;
}
.pcalc-copy:hover { border-color: rgb(var(--primary-6, 22, 93, 255)); color: rgb(var(--primary-6, 22, 93, 255)); }
.pcalc-dtnote { display: flex; align-items: flex-start; }
.pcalc-dtnote .pcalc-badge--dt { margin-top: 1px; }

/* ---------- BIT 位选择 ---------- */
.pcalc-bits { display: flex; align-items: center; gap: 3px; margin-top: 8px; flex-wrap: wrap; }
.pcalc-bits__label { font-size: 11px; color: var(--color-text-3, #86909c); }
.pcalc-bits__btn {
  border: 1px solid var(--color-border-2, #e5e6eb);
  background: var(--color-fill-1, #f7f8fa);
  color: var(--color-text-2, #4e5969);
  font-size: 10px; padding: 2px 7px; border-radius: 4px; cursor: pointer;
  font-family: ui-monospace, Consolas, monospace;
  transition: all 0.12s;
}
.pcalc-bits__btn.is-on {
  border-color: rgb(var(--primary-6, 22, 93, 255));
  color: rgb(var(--primary-6, 22, 93, 255));
  background: var(--color-primary-light-1, #e8f3ff);
  font-weight: 700;
}

/* ---------- 同值全字序对照 ---------- */
.pcalc-orders { margin-top: 12px; padding-top: 10px; border-top: 1px dashed var(--color-border-2, #e5e6eb); }
.pcalc-orders__head {
  display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap;
  font-size: 11px; font-weight: 600; color: var(--color-text-2, #4e5969); margin-bottom: 6px;
}
.pcalc-orders__note { font-weight: 400; font-size: 10px; color: var(--color-text-4, #c9cdd4); }
.pcalc-orders__empty { font-size: 11px; color: var(--color-text-4, #c9cdd4); }
.pcalc-orders__group { margin-bottom: 8px; }
.pcalc-orders__group:last-child { margin-bottom: 0; }
.pcalc-orders__group-label { font-size: 10px; color: var(--color-text-4, #c9cdd4); margin: 2px 0 3px; }
.pcalc-orders__row {
  display: flex; align-items: center; justify-content: space-between; gap: 8px;
  width: 100%; padding: 3px 8px;
  border: none; border-radius: 5px; cursor: pointer;
  background: transparent; text-align: left;
  transition: background 0.12s;
}
.pcalc-orders__row:hover { background: var(--color-fill-2, #f2f3f5); }
.pcalc-orders__row.is-active {
  background: var(--color-primary-light-1, #e8f3ff);
  box-shadow: inset 2px 0 0 rgb(var(--primary-6, 22, 93, 255));
}
.pcalc-orders__id {
  font-size: 11px; font-family: ui-monospace, Consolas, monospace;
  color: var(--color-text-2, #4e5969); white-space: nowrap;
}
.pcalc-orders__row.is-active .pcalc-orders__id { color: rgb(var(--primary-6, 22, 93, 255)); font-weight: 700; }
.pcalc-orders__wire {
  font-size: 13px; font-family: ui-monospace, Consolas, monospace;
  color: var(--color-text-1, #1d2129); letter-spacing: 0.5px; white-space: pre;
}
.pcalc-orders__row.is-active .pcalc-orders__wire { color: rgb(var(--primary-6, 22, 93, 255)); font-weight: 700; }
.pcalc-orders__val {
  font-size: 13px; font-family: ui-monospace, Consolas, monospace;
  color: var(--color-text-1, #1d2129); white-space: nowrap;
}
.pcalc-orders__row.is-active .pcalc-orders__val { color: rgb(var(--primary-6, 22, 93, 255)); font-weight: 700; }

/* ---------- 拖拽调整大小手柄 ---------- */
.pcalc-resize { position: absolute; z-index: 5; touch-action: none; }
.pcalc-resize--e { right: -3px; top: 40px; bottom: 10px; width: 8px; cursor: ew-resize; }
.pcalc-resize--s { left: 14px; right: 14px; bottom: -3px; height: 8px; cursor: ns-resize; }
.pcalc-resize--se { right: -2px; bottom: -2px; width: 18px; height: 18px; cursor: nwse-resize; }
.pcalc-resize--se::after {
  content: '';
  position: absolute; right: 4px; bottom: 4px;
  width: 8px; height: 8px;
  border-right: 2px solid var(--color-text-4, #c9cdd4);
  border-bottom: 2px solid var(--color-text-4, #c9cdd4);
  border-radius: 0 0 3px 0;
  transition: border-color 0.15s;
}
.pcalc-resize--e:hover, .pcalc-resize--s:hover { background: rgba(var(--primary-6, 22, 93, 255), 0.08); }
.pcalc-resize--se:hover::after { border-color: rgb(var(--primary-6, 22, 93, 255)); }

/* ---------- Select 弹层容器（高于面板 z-index 3000） ---------- */
#pcalc-popup-root {
  position: fixed;
  inset: 0;
  z-index: 3100;
  pointer-events: none;
}
/* 容器不拦截点击，弹层本身恢复可交互 */
#pcalc-popup-root :deep(.arco-trigger-popup) { pointer-events: auto; }
</style>
