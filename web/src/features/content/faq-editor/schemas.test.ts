import { describe, expect, it } from 'vitest'
import { faqFormSchema, faqNewSearchSchema } from './schemas'

const validFAQ = {
  title: '退款政策',
  questions: ['如何申请退款？'],
  answer: '请在订单页提交申请。',
}

describe('faqFormSchema', () => {
  it('requires a trimmed title, at least one non-empty question and an answer', () => {
    const result = faqFormSchema.safeParse({
      title: '   ',
      questions: [],
      answer: '\n\t',
    })

    expect(result.success).toBe(false)
    if (result.success) return
    const errors = result.error.flatten()
    expect(errors.fieldErrors.title).toContain('请输入 FAQ 标题')
    expect(errors.fieldErrors.questions).toContain('至少添加一个问题')
    expect(errors.fieldErrors.answer).toContain('请输入回答')
  })

  it('rejects blank and normalized duplicate question variants', () => {
    expect(
      faqFormSchema.safeParse({
        ...validFAQ,
        questions: ['　\t'],
      }).success
    ).toBe(false)

    const duplicate = faqFormSchema.safeParse({
      ...validFAQ,
      questions: [' How   To Refund? ', 'how to refund?'],
    })
    expect(duplicate.success).toBe(false)
    if (duplicate.success) return
    expect(
      duplicate.error.issues.some((issue) => issue.message === '问题不能重复')
    ).toBe(true)
  })

  it('trims payload values while preserving question order', () => {
    expect(
      faqFormSchema.parse({
        title: ' 退款政策 ',
        questions: [' 第一个问题 ', '第二个问题  '],
        answer: ' 回答内容 ',
      })
    ).toEqual({
      title: '退款政策',
      questions: ['第一个问题', '第二个问题'],
      answer: '回答内容',
    })
  })
})

describe('faqNewSearchSchema（健康面板零召回预填，v1.4.0 §9.3.6）', () => {
  it('accepts an optional question within the 500-char limit', () => {
    expect(faqNewSearchSchema.parse({})).toEqual({})
    expect(faqNewSearchSchema.parse({ question: '发票抬头怎么改' })).toEqual({
      question: '发票抬头怎么改',
    })
    expect(
      faqNewSearchSchema.safeParse({ question: '问'.repeat(500) }).success
    ).toBe(true)
  })

  it('rejects a question longer than 500 chars', () => {
    expect(
      faqNewSearchSchema.safeParse({ question: '问'.repeat(501) }).success
    ).toBe(false)
  })
})
