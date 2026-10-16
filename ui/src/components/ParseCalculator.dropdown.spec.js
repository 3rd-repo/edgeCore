// 验证 ParseCalculator 预设下拉分组完整渲染（回归：插槽 a-optgroup 曾只渲染 1 项）
import { describe, it, expect } from 'vitest'
import { mount, config } from '@vue/test-utils'
import ArcoVue from '@arco-design/web-vue'
import ParseCalculator from './ParseCalculator.vue'
import { formatPresets } from '../utils/formatPresets'

config.global.plugins = [ArcoVue]

describe('ParseCalculator preset dropdown', () => {
  it('options prop 包含全部 26 个预设（4 组）', () => {
    const wrapper = mount(ParseCalculator, {
      props: { visible: true },
      global: { plugins: [ArcoVue] }
    })
    const select = wrapper.findComponent({ name: 'Select' })
    expect(select.exists()).toBe(true)
    const groups = select.props('options')
    expect(groups.length).toBe(4)
    const total = groups.reduce((n, g) => n + g.options.length, 0)
    expect(total).toBe(formatPresets.length)
    // 组标题与每组数量
    expect(groups.map(g => g.options.length)).toEqual([4, 6, 12, 4])
    // 关键成员抽查
    const ids = groups.flatMap(g => g.options.map(o => o.value))
    for (const id of ['Signed_AB', 'Signed_BA', 'Unsigned_ABCD', 'Float_CDAB', 'Double_GHEFCDAB']) {
      expect(ids).toContain(id)
    }
  })

  it('打开下拉后 DOM 中渲染全部选项', async () => {
    const wrapper = mount(ParseCalculator, {
      props: { visible: true },
      global: { plugins: [ArcoVue] },
      attachTo: document.body
    })
    const select = wrapper.findComponent({ name: 'Select' })
    await select.find('.arco-select').trigger('click')
    await new Promise(r => setTimeout(r, 50))
    const optionEls = document.querySelectorAll('.arco-select-option')
    // jsdom 下 Arco 虚拟列表对同一选项渲染两份 DOM，按文本去重后校验
    const texts = new Set(Array.from(optionEls).map(el => el.textContent.trim()))
    expect(texts.size).toBe(formatPresets.length)
    const groupTitles = new Set(Array.from(document.querySelectorAll('.arco-select-group-title')).map(el => el.textContent.trim()))
    expect(groupTitles.size).toBe(4)
    wrapper.unmount()
    document.body.innerHTML = ''
  })
})
