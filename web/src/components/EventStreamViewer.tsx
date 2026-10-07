import React, { useState } from 'react';
import { KafkaEvent } from '../engine/types';
import { Copy, Radio } from 'lucide-react';
import { useLanguage } from '../i18n/LanguageContext';

interface EventStreamViewerProps {
  events: KafkaEvent[];
}

export const EventStreamViewer: React.FC<EventStreamViewerProps> = ({ events }) => {
  const { t } = useLanguage();
  const [selectedTopic, setSelectedTopic] = useState<string>('ALL');
  const [expandedIndex, setExpandedIndex] = useState<number | null>(null);

  const filteredEvents = selectedTopic === 'ALL'
    ? events
    : events.filter((e) => e.topic === selectedTopic);

  const toggleExpand = (idx: number) => {
    setExpandedIndex(expandedIndex === idx ? null : idx);
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
  };

  return (
    <div className="bg-slate-900/60 border border-slate-800 rounded-lg p-4 sm:p-5">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between pb-4 border-b border-slate-800/80 gap-3">
        <div>
          <div className="flex items-center gap-2">
            <h2 className="text-base font-bold text-white tracking-wide flex items-center gap-2">
              <Radio className="w-4 h-4 text-emerald-400 animate-pulse" />
              {t.kafka.title}
            </h2>
            <span className="text-xs font-mono text-slate-400 bg-slate-900 px-2 py-0.5 rounded border border-slate-800">
              {t.kafka.commitLog}
            </span>
          </div>
          <p className="text-xs text-slate-400 mt-1">
            {t.kafka.subtitle}
          </p>
        </div>

        {/* Topic Filters */}
        <div className="flex items-center gap-1.5 p-0.5 bg-slate-950 border border-slate-800 rounded-md text-xs">
          {['ALL', 'orders.placement', 'orders.events', 'market.depth'].map((topic) => (
            <button
              key={topic}
              onClick={() => setSelectedTopic(topic)}
              className={`px-2.5 py-1 rounded font-mono text-[11px] transition-colors ${
                selectedTopic === topic
                  ? 'bg-slate-800 text-emerald-400 font-semibold'
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              {topic === 'ALL' ? t.kafka.allTopics : topic}
            </button>
          ))}
        </div>
      </div>

      {/* Stream List */}
      <div className="mt-4 space-y-2 max-h-[560px] overflow-y-auto font-mono">
        {filteredEvents.map((evt, idx) => {
          const isExpanded = expandedIndex === idx;
          const timeStr = new Date(evt.timestamp).toLocaleTimeString('vi-VN', {
            hour12: false,
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
            fractionalSecondDigits: 3,
          } as any);

          const getBadgeColor = () => {
            switch (evt.eventType) {
              case 'OrderPlaced':
                return 'bg-blue-950/80 text-blue-300 border-blue-800';
              case 'OrderMatched':
                return 'bg-emerald-950/80 text-emerald-300 border-emerald-800';
              case 'OrderCanceled':
                return 'bg-amber-950/80 text-amber-300 border-amber-800';
              case 'MarketDepth':
                return 'bg-purple-950/80 text-purple-300 border-purple-800';
              default:
                return 'bg-slate-900 text-slate-300 border-slate-800';
            }
          };

          return (
            <div
              key={`${evt.timestamp}-${idx}`}
              className="bg-slate-950/80 border border-slate-800/80 rounded-md p-2.5 text-xs transition-colors hover:border-slate-700"
            >
              <div
                className="flex items-center justify-between cursor-pointer"
                onClick={() => toggleExpand(idx)}
              >
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="text-[11px] text-slate-500 tabular-nums">{timeStr}</span>
                  <span className="text-slate-400 font-semibold text-[11px] bg-slate-900 px-1.5 py-0.5 rounded border border-slate-800">
                    topic: {evt.topic}
                  </span>
                  <span className={`text-[11px] px-1.5 py-0.5 rounded border ${getBadgeColor()}`}>
                    {evt.eventType}
                  </span>
                  <span className="text-slate-400 text-[11px]">
                    PartitionKey: <strong className="text-emerald-400">{evt.partitionKey}</strong>
                  </span>
                </div>

                <span className="text-[11px] text-slate-500 hover:text-slate-300">
                  {isExpanded ? t.kafka.hideJson : t.kafka.inspectJson}
                </span>
              </div>

              {isExpanded && (
                <div className="mt-2.5 pt-2 border-t border-slate-800/80">
                  <div className="flex items-center justify-between text-[11px] text-slate-400 mb-1">
                    <span>Message Payload (Watermill UUID / JSON):</span>
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        copyToClipboard(JSON.stringify(evt.payload, null, 2));
                      }}
                      className="flex items-center gap-1 hover:text-slate-200"
                    >
                      <Copy className="w-3 h-3" />
                      {t.kafka.copyPayload}
                    </button>
                  </div>
                  <pre className="bg-slate-900/90 text-slate-300 p-2.5 rounded text-[11px] overflow-x-auto max-h-48 border border-slate-800">
                    {JSON.stringify(evt.payload, null, 2)}
                  </pre>
                </div>
              )}
            </div>
          );
        })}

        {filteredEvents.length === 0 && (
          <div className="text-xs text-slate-500 text-center py-12">
            {t.kafka.noEvents}
          </div>
        )}
      </div>
    </div>
  );
};
