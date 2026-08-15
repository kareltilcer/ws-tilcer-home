import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeSanitize from 'rehype-sanitize'

// MarkdownView renders untrusted user markdown (card notes, event descriptions,
// long log diffs) safely — rehype-sanitize strips dangerous HTML.
export function MarkdownView({ children }: { children: string }) {
  if (!children.trim()) {
    return <p className="text-sm text-subtle italic">Bez poznámek.</p>
  }
  return (
    <div className="space-y-2 text-sm leading-relaxed text-fg [&_a]:text-accent [&_a]:underline [&_code]:font-mono [&_code]:text-[13px] [&_img]:my-2 [&_img]:max-w-full [&_img]:rounded-lg [&_ul]:list-disc [&_ul]:pl-5 [&_ol]:list-decimal [&_ol]:pl-5 [&_strong]:font-semibold [&_table]:my-2 [&_table]:border-collapse [&_table]:text-[13px] [&_th]:border [&_th]:border-border [&_th]:bg-s2 [&_th]:px-3 [&_th]:py-1.5 [&_th]:text-left [&_th]:font-semibold [&_td]:border [&_td]:border-border [&_td]:px-3 [&_td]:py-1.5">
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeSanitize]}>
        {children}
      </ReactMarkdown>
    </div>
  )
}
