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
    <div className="space-y-2 text-sm leading-relaxed text-fg [&_a]:text-accent [&_a]:underline [&_code]:font-mono [&_code]:text-[13px] [&_ul]:list-disc [&_ul]:pl-5 [&_ol]:list-decimal [&_ol]:pl-5 [&_strong]:font-semibold">
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeSanitize]}>
        {children}
      </ReactMarkdown>
    </div>
  )
}
