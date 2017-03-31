
function init() {
    list();
}

function list() {
    $.getJSON("/list", function (data) {
        if (data.result) {
            $("#list").empty();
            for (order of data.result) {
                let tr = $("<tr>");
                let item = $("<td>").text(order.Item);
                let addr = $("<td>").text(order.Addr);
                let price = $("<td>").text(order.Price);
                let asset = $("<td>").text(order.Asset);
                let status = $("<td>").text(getStatus(order.Status));
                let to = $("<td>").text(formatDate(order.Timeout));
                let lm = $("<td>").text(formatDate(order.LastModify));
                tr.append(item).append(addr).append(price).append(asset).append(status).append(to).append(lm);
                $("#list").prepend(tr);
            }
            $("#lm").text(formatDate(Math.floor((new Date()).getTime() / 1000)));
        }
    });
    setTimeout(list, 3000);
}

function getStatus(status) {
    let msg = "Unknown";
    if (status == 1) {
        msg = "Paid";
    } else if (status == 0) {
        msg = "Wait";
    } else if (status == -1) {
        msg = "Timeout";
    }
    return msg;
}

function formatDate(unixTimestamp) {
    let date = new Date(unixTimestamp * 1000);
    return ""
        // + date.getFullYear() + "/" 
        // + ('0' + (date.getMonth() + 1)).slice(-2) + "/" 
        // + ('0' + date.getDate()).slice(-2) + " " 
        + ('0' + date.getHours()).slice(-2) + ":"
        + ('0' + date.getMinutes()).slice(-2) + ":"
        + ('0' + date.getSeconds()).slice(-2);
}

$(init);