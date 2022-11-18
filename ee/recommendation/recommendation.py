from utils.ch_client import ClickHouseClient
from utils.pg_client import PostgresClient

def get_features_clickhouse(**kwargs):
    if 'limit' in kwargs:
        limit = kwargs['limit']
    else:
        limit = 500
    query = f"""SELECT session_id, project_id, user_id, events_count, errors_count, duration, country, issue_score, device_type, rage, jsexception, badrequest FROM (
    SELECT session_id, project_id, user_id, events_count, errors_count, duration, toInt8(user_country) as country, issue_score, toInt8(user_device_type) as device_type FROM experimental.sessions WHERE user_id IS NOT NULL) as T1
INNER JOIN (SELECT session_id, project_id, sum(issue_type = 'click_rage') as rage, sum(issue_type = 'js_exception') as jsexception, sum(issue_type = 'bad_request') as badrequest FROM experimental.events WHERE event_type = 'ISSUE' AND session_id > 0 GROUP BY session_id, project_id LIMIT {limit}) as T2
ON T1.session_id = T2.session_id AND T1.project_id = T2.project_id;"""
    with ClickHouseClient() as conn:
        res = conn.execute(query)
    return res


def query_funnels(*kwargs):
    # If public.funnel is empty
    funnels_query = f"""SELECT project_id, user_id, filter FROM (SELECT project_id, user_id, metric_id FROM public.metrics WHERE metric_type='funnel'
    ) as T1 LEFT JOIN (SELECT filter, metric_id FROM public.metric_series) as T2 ON T1.metric_id = T2.metric_id"""
    # Else
    # funnels_query = "SELECT project_id, user_id, filter FROM public.funnels"

    with PostgresClient() as conn:
        conn.execute(funnels_query)
        res = conn.fetchall()
    return res


def query_metrics(*kwargs):
    metrics_query = """SELECT metric_type, metric_of, metric_value, metric_format FROM public.metrics"""
    with PostgresClient() as conn:
        conn.execute(metrics_query)
        res = conn.fetchall()
    return res


def query_with_filters(*kwargs):
    filters_query = """SELECT T1.metric_id as metric_id, project_id, name, metric_type, metric_of, filter FROM (
    SELECT metric_id, project_id, name, metric_type, metric_of FROM metric_series WHERE filter != '{}') as T1 INNER JOIN
    (SELECT metric_id, filter FROM metrics) as T2 ON T1.metric_id = T2.metric_id"""
    with PostgresClient() as conn:
        conn.execute(filters_query)
        res = conn.fetchall()
    return res


def transform_funnel(project_id, user_id, data):
    res = list()
    for k in range(len(data)):
        _tmp = data[k]
        if _tmp['project_id'] != project_id or _tmp['user_id'] != user_id:
            continue
        else:
            _tmp = _tmp['filter']['events']
            res.append(_tmp)
    return res


def transform_with_filter(data, *kwargs):
    res = list()
    for k in range(len(data)):
        _tmp = data[k]
        jump = False
        for _key in kwargs.keys():
            if data[_key] != kwargs[_key]:
                jump = True
                break
        if jump:
            continue
        _type = data['metric_type']
        if _type == 'funnel':
            res.append(['funnel', _tmp['filter']['events']])
        elif _type == 'timeseries':
            res.append(['timeseries', _tmp['filter']['filters'], _tmp['filter']['events']])
        elif _type == 'table':
            res.append(['table', _tmp['metric_of'], _tmp['filter']['events']])
    return res


def transform_data():
    pass


def transform(element):
    key_ = element.pop('user_id')
    secondary_key_ = element.pop('session_id')
    context_ = element.pop('project_id')
    features_ = element
    del element
    return {(key_, context_): {secondary_key_: list(features_.values())}}


def get_by_project(data, project_id):
    head_ = [list(d.keys())[0][1] for d in data]
    index_ = [k for k in range(len(head_)) if head_[k] == project_id]
    return [data[k] for k in index_]


def get_by_user(data, user_id):
    head_ = [list(d.keys())[0][0] for d in data]
    index_ = [k for k in range(len(head_)) if head_[k] == user_id]
    return [data[k] for k in index_]


if __name__ == '__main__':
    data = get_features_clickhouse()
    print('Data length:', len(data))