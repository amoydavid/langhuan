import { describe, expect, it } from 'vitest'
import { faqFormSchema } from './schemas'

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
