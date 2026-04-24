import { JARGON } from '../utils/jargon';

interface Props {
  term: string;
  children: React.ReactNode;
}

export default function Jargon({ term, children }: Props) {
  const title = JARGON[term] ?? term;
  return <abbr title={title} style={{ textDecoration: 'underline dotted', cursor: 'help' }}>{children}</abbr>;
}
