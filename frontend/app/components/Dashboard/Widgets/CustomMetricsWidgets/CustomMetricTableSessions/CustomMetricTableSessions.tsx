import { useObserver } from "mobx-react-lite";
import React from "react";
import SessionItem from "Shared/SessionItem";
import { Pagination, NoContent } from "UI";
import { useStore } from "App/mstore";
import { overPastString } from "App/dateRange";

interface Props {
    metric: any;
    isTemplate?: boolean;
    isEdit?: boolean;
}

function CustomMetricTableSessions(props: Props) {
    const { isEdit = false, metric } = props;
    const { dashboardStore } = useStore();
    const period = dashboardStore.period;

    return useObserver(() => (
        <NoContent
            show={
                !metric ||
                !metric.data ||
                !metric.data.sessions ||
                metric.data.sessions.length === 0
            }
            size="small"
            title={`No sessions found ${overPastString(period)}`}
        >
            <div className="pb-4">
                {metric.data.sessions &&
                    metric.data.sessions.map((session: any, index: any) => (
                        <div
                            className="border-b last:border-none"
                            key={session.sessionId}
                        >
                            <SessionItem session={session} />
                        </div>
                    ))}

                {isEdit && (
                    <div className="mt-6 flex items-center justify-center">
                        <Pagination
                            page={metric.page}
                            totalPages={Math.ceil(
                                metric.data.total / metric.limit
                            )}
                            onPageChange={(page: any) =>
                                metric.updateKey("page", page)
                            }
                            limit={metric.data.total}
                            debounceRequest={500}
                        />
                    </div>
                )}

                {!isEdit && (
                    <ViewMore total={metric.data.total} limit={metric.limit} />
                )}
            </div>
        </NoContent>
    ));
}

export default CustomMetricTableSessions;

const ViewMore = ({ total, limit }: any) =>
    total > limit && (
        <div className="mt-4 flex items-center justify-center cursor-pointer w-fit mx-auto">
            <div className="text-center">
                <div className="color-teal text-lg">
                    All <span className="font-medium">{total}</span> sessions
                </div>
            </div>
        </div>
    );